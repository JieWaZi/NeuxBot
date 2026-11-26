package agent

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

const detachedScratchPrefix = "neux-detached-"

// RecoverEphemeralScratch reclaims only directories owned by this Core data root.
func RecoverEphemeralScratch(root string) error {
	if !filepath.IsAbs(root) {
		return status_error.RuntimeConfigurationInvalid
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return status_error.RuntimeConfigurationInvalid
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return status_error.RuntimeConfigurationInvalid
	}
	var failure error
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), detachedScratchPrefix) && entry.IsDir() {
			if err = os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
				failure = status_error.RuntimeShutdownFailed
			}
		}
	}
	return failure
}

// RetryEphemeralCleanup reclaims failed review leases while Core continues to serve other work.
func (r *ProviderRuntime) RetryEphemeralCleanup(ctx context.Context) error {
	r.mu.Lock()
	var closing []*providerConnection
	for _, conn := range r.connections {
		if !conn.ephemeral {
			continue
		}
		conn.mu.Lock()
		if conn.closing {
			closing = append(closing, conn)
		}
		conn.mu.Unlock()
	}
	r.mu.Unlock()
	return errors.Join(closeProviderConnections(ctx, closing), r.retryEphemeralScratch(ctx))
}

func (r *ProviderRuntime) closeEphemeralScratch(path string) error {
	err := os.RemoveAll(path)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		if r.ephemeralScratch == nil {
			r.ephemeralScratch = make(map[string]struct{})
		}
		r.ephemeralScratch[path] = struct{}{}
		return status_error.RuntimeShutdownFailed
	}
	delete(r.ephemeralScratch, path)
	return nil
}

func (r *ProviderRuntime) retryEphemeralScratch(ctx context.Context) error {
	r.mu.Lock()
	paths := make([]string, 0, len(r.ephemeralScratch))
	for path := range r.ephemeralScratch {
		paths = append(paths, path)
	}
	r.mu.Unlock()
	var failure error
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.closeEphemeralScratch(path); err != nil {
			failure = err
		}
	}
	return failure
}

// EphemeralClaudeMode routes a managed, no-history launch of the existing adapter.
const EphemeralClaudeMode = "__neuxbot-ephemeral-claude"

func (launch ProviderLaunch) usesEphemeralClaude(provider string) bool {
	return launch.EphemeralText && launch.RequireNoTools && provider == constants.RuntimeIDClaude &&
		filepath.IsAbs(launch.Executable) && len(launch.PrefixArgs) == 0
}

// RunEphemeralClaude forwards the existing SDK transport without persisting a native session.
// The pinned ACP adapter reserves native launch flags, so Core appends its
// fixed no-history policy at the launch boundary rather than accepting it
// from a runtime profile or creating a second protocol adapter.
func RunEphemeralClaude(arguments []string) int {
	if len(arguments) < 2 || !filepath.IsAbs(arguments[0]) {
		return 2
	}
	args, ok := ephemeralClaudeArgs(arguments[1:])
	if !ok {
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if slices.Equal(args, []string{"--version"}) {
		check, cancel := context.WithTimeout(ctx, 3*time.Second)
		help, err := exec.CommandContext(check, arguments[0], "--help").Output()
		cancel()
		if err != nil || !bytes.Contains(help, []byte("--no-session-persistence")) || !bytes.Contains(help, []byte("--tools")) {
			return 2
		}
	}
	command := exec.CommandContext(ctx, arguments[0], args...)
	command.Stdin, command.Stdout = os.Stdin, os.Stdout
	// Private generation diagnostics can contain input fragments.
	if err := command.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		return 1
	}
	return 0
}

func ephemeralClaudeArgs(args []string) ([]string, bool) {
	if slices.Equal(args, []string{"--version"}) {
		return args, true
	}
	if !slices.Contains(args, "--input-format") || !slices.Contains(args, "--output-format") ||
		!slices.Contains(args, "--tools=") {
		return nil, false
	}
	const emptyMCP = `{"mcpServers":{}}`
	suppliedMCP := false
	for i, argument := range args {
		flag, value, inline := strings.Cut(argument, "=")
		switch flag {
		case "--mcp-config":
			if !inline && i+1 < len(args) {
				value = args[i+1]
			}
			if suppliedMCP || value != emptyMCP {
				return nil, false
			}
			suppliedMCP = true
		case "--resume", "--continue", "-r", "-c", "--fork-session", "--add-dir", "--settings", "--managed-settings":
			return nil, false
		}
	}
	args = slices.Clone(args)
	if !slices.Contains(args, "--print") && !slices.Contains(args, "-p") {
		args = append(args, "--print")
	}
	if !suppliedMCP {
		args = append(args, "--mcp-config", emptyMCP)
	}
	if !slices.Contains(args, "--strict-mcp-config") {
		args = append(args, "--strict-mcp-config")
	}
	return append(args, "--no-session-persistence"), true
}

// ephemeralTextStore holds detached response bodies in memory while reusing
// the existing ordinary Store for body-free admission and canonical usage.
type ephemeralTextStore struct {
	// ConversationStore is used only for attribution and terminal metadata.
	ConversationStore
	// mu guards the private response cache.
	mu sync.Mutex
	// results contains only this one detached lifecycle's native responses.
	results map[NativeResultKey]NativeResult
	// answerKey links the streamed answer to its native invocation.
	answerKey NativeResultKey
	// answerMessageID identifies the native message supplying the streamed answer.
	answerMessageID string
	// answer retains streamed text for adapters without a final body in the receipt.
	answer strings.Builder
	// answerTooLarge records that the streamed response exceeded its byte budget.
	answerTooLarge bool
}

func (*ephemeralTextStore) PrepareNativeToolUpdate(context.Context, NativeFragmentBatch) (NativeFragmentBatch, error) {
	return NativeFragmentBatch{}, status_error.RuntimePolicyUnsupported
}

func (s *ephemeralTextStore) CommitNativeFragments(_ context.Context, b NativeFragmentBatch) (NativeFragmentRange, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, change := range b.Changes {
		current := s.answerKey == b.Key && s.answerMessageID == b.MessageID
		if change.ContentType == constants.RuntimeEventTypeCommentaryDelta && current {
			s.answer.Reset()
			s.answerTooLarge = false
		}
		if change.ContentType != constants.RuntimeEventTypeTextDelta ||
			(change.Kind != MessageAppend && change.Kind != MessageReplace) {
			continue
		}
		if !current || change.Kind == MessageReplace {
			s.answerKey, s.answerMessageID = b.Key, b.MessageID
			s.answer.Reset()
			s.answerTooLarge = false
		}
		if s.answer.Len()+len(change.Text) > maxCorrectionBytes {
			s.answerTooLarge = true
			s.answer.Reset()
		}
		if !s.answerTooLarge {
			s.answer.WriteString(change.Text)
		}
	}
	return NativeFragmentRange{Key: b.Key, MessageID: b.MessageID}, nil
}

func (s *ephemeralTextStore) streamedText(key NativeResultKey) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if key != s.answerKey || s.answerTooLarge {
		return ""
	}
	return s.answer.String()
}

func (*ephemeralTextStore) ReadNativeFragments(context.Context, NativeFragmentRange) ([]MessageChange, error) {
	return nil, status_error.RuntimePolicyUnsupported
}

func (s *ephemeralTextStore) CommitNativeResult(ctx context.Context, v NativeResult) error {
	if err := s.ConversationStore.CommitNativeResult(ctx, ephemeralUsageResult(v)); err != nil {
		return err
	}
	s.mu.Lock()
	s.results[v.Key] = v
	s.mu.Unlock()
	return nil
}

func (s *ephemeralTextStore) ReadNativeResult(_ context.Context, key NativeResultKey) (NativeResult, error) {
	s.mu.Lock()
	value, ok := s.results[key]
	s.mu.Unlock()
	if !ok {
		return NativeResult{}, status_error.ChatLogNativeOutcomePending
	}
	return value, nil
}

func ephemeralUsageResult(v NativeResult) NativeResult {
	return NativeResult{Key: v.Key, Turn: v.Turn, Context: v.Context, Attribution: v.Attribution}
}
