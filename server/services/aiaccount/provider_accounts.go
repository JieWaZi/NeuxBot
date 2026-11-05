package aiaccount

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// ResolveAccount freezes the provider's actual credential identity for admission or model discovery.
func (s *Service) ResolveAccount(ctx context.Context, request agent.AccountRequest, systemHome string) (agent.AccountRequest, error) {
	switch request.Provider {
	case constants.RuntimeIDCodex:
		return s.ResolveCodexAccount(ctx, request, systemHome)
	case constants.RuntimeIDClaude:
		if request.AccountID == agent.SystemAccountID {
			return resolveClaudeSystemAccount(ctx, request, systemHome)
		}
		return s.resolveManagedClaudeAccount(ctx, request)
	case constants.RuntimeIDCursor:
		return resolveCursorAccount(ctx, request, systemHome)
	case constants.RuntimeIDWorkBuddy, constants.RuntimeIDQoder, constants.RuntimeIDQoderCN:
		return s.resolveACPSystemAccount(ctx, request, systemHome)
	case constants.RuntimeIDKimi, constants.RuntimeIDGemini, constants.RuntimeIDGrok, constants.RuntimeIDOpenCode, constants.RuntimeIDPi:
		if request.AccountID == agent.SystemAccountID {
			return resolveNativeCLISystemAccount(ctx, request, systemHome)
		}
		release, err := s.acquireNativeCLIRead(ctx, request)
		if err != nil {
			return agent.AccountRequest{}, err
		}
		defer release()
		resolved, _, _, err := s.resolveManagedNativeCLI(ctx, request)
		return resolved, err
	default:
		return agent.AccountRequest{}, status_error.RuntimeNotRegistered
	}
}

// AcquireAccount rechecks a frozen selection and keeps managed resources leased until native exit.
func (s *Service) AcquireAccount(ctx context.Context, request agent.AccountRequest) (agent.AccountLease, error) {
	switch request.Provider {
	case constants.RuntimeIDCodex:
		return s.AcquireCodexAccount(ctx, request)
	case constants.RuntimeIDClaude:
		if request.AccountID == agent.SystemAccountID {
			return acquireClaudeSystemAccount(ctx, request)
		}
		return s.acquireManagedClaudeAccount(ctx, request)
	case constants.RuntimeIDCursor:
		return acquireCursorAccount(ctx, request)
	case constants.RuntimeIDWorkBuddy, constants.RuntimeIDQoder, constants.RuntimeIDQoderCN:
		return s.acquireACPSystemAccount(ctx, request)
	case constants.RuntimeIDKimi, constants.RuntimeIDGemini, constants.RuntimeIDGrok, constants.RuntimeIDOpenCode, constants.RuntimeIDPi:
		if request.AccountID == agent.SystemAccountID {
			return acquireNativeCLISystemAccount(ctx, request)
		}
		return s.acquireManagedNativeCLI(ctx, request)
	default:
		return agent.AccountLease{}, status_error.RuntimeNotRegistered
	}
}

// SetAccountAuthInvalidator attaches the native connection and cache owner.
func (s *Service) SetAccountAuthInvalidator(invalidate func(context.Context, string, string, uint64)) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.invalidateAccountAuth = invalidate
}

// InvalidateAccountAuth fences the previous credential generation after a durable mutation.
func (s *Service) InvalidateAccountAuth(ctx context.Context, previous models.AIAccount) {
	s.mutex.Lock()
	invalidate := s.invalidateAccountAuth
	s.mutex.Unlock()
	if invalidate == nil {
		return
	}
	for _, provider := range []string{
		constants.RuntimeIDCodex, constants.RuntimeIDClaude, constants.RuntimeIDKimi,
		constants.RuntimeIDGemini, constants.RuntimeIDGrok, constants.RuntimeIDOpenCode, constants.RuntimeIDPi,
	} {
		if _, ok := accountEndpoint(previous, provider); ok {
			invalidate(ctx, provider, previous.ID, previous.AuthGeneration)
		}
	}
}

func isManagedAccountID(id string) bool {
	decoded, err := hex.DecodeString(id)
	return err == nil && len(decoded) == 16 && hex.EncodeToString(decoded) == id
}

// managedAccountHome uses the same identity and directory boundary for both managed providers.
func (s *Service) managedAccountHome(provider string, account models.AIAccount, create bool, subdirectory ...string) (string, error) {
	if !isManagedAccountID(account.ID) || account.AuthGeneration == 0 || !filepath.IsAbs(s.dataDirectory) {
		return "", status_error.AIAccountConfigurationInvalid
	}
	root, err := filepath.EvalSymlinks(s.dataDirectory)
	if err != nil {
		return "", managedAccountStorageError(provider, account.ID, err)
	}
	parts := append([]string{"ai-accounts", provider, account.ID, strconv.FormatUint(account.AuthGeneration, 10)}, subdirectory...)
	directory := root
	for _, part := range parts {
		directory = filepath.Join(directory, part)
		if create {
			if err := os.Mkdir(directory, 0700); err != nil && !errors.Is(err, os.ErrExist) {
				return "", managedAccountStorageError(provider, account.ID, err)
			}
		}
		info, err := os.Lstat(directory)
		if !create && errors.Is(err, os.ErrNotExist) {
			return filepath.Join(root, filepath.Join(parts...)), nil
		}
		if err != nil {
			return "", managedAccountStorageError(provider, account.ID, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", status_error.AIAccountWorkspaceError
		}
		if create && info.Mode().Perm() != 0700 {
			if err := os.Chmod(directory, 0700); err != nil {
				return "", managedAccountStorageError(provider, account.ID, err)
			}
		}
	}
	return directory, nil
}

func managedAccountStorageError(provider, id string, err error) error {
	logx.Errorf("managed account storage failed: provider=%s account_id=%s error=%v", provider, id, err)
	return status_error.AIAccountWorkspaceError
}

// systemAccountGeneration is fixed because native system credential changes supply a different subject.
const systemAccountGeneration = 1

func accountSubject(namespace string, components ...string) string {
	scope, _ := json.Marshal(components)
	digest := sha256.Sum256(scope)
	return namespace + ":" + hex.EncodeToString(digest[:])
}

func nativeAccountEnvironment(overrides map[string]string) []string {
	base := accountconfig.Environment(os.Environ())
	env := base[:0]
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if _, overridden := overrides[strings.ToUpper(key)]; !overridden {
			env = append(env, entry)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(overrides)) {
		if value := overrides[key]; value != "" {
			env = append(env, key+"="+value)
		}
	}
	return env
}

func nativeAccountStatusError(ctx context.Context, provider string, err error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Native status output and failure text can contain account labels or credentials.
	logx.Errorf("native account status failed: provider=%s error_type=%T", provider, err)
	return status_error.RuntimeUnavailable
}

// systemAccountHome resolves a system directory without modifying it. A CLI can own a
// configuration directory that is created only when it first needs to save settings.
func systemAccountHome(path string, allowAbsent bool) (string, error) {
	if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return "", status_error.RuntimeConfigurationInvalid
	}
	path = filepath.Clean(path)
	canonical, err := filepath.EvalSymlinks(path)
	if errors.Is(err, os.ErrNotExist) {
		if !allowAbsent {
			return "", status_error.RuntimeAuthRequired
		}
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) || filepath.Dir(path) == path {
			return "", status_error.RuntimeConfigurationInvalid
		}
		parent, err := systemAccountHome(filepath.Dir(path), true)
		if err != nil {
			return "", err
		}
		return filepath.Join(parent, filepath.Base(path)), nil
	}
	if err != nil {
		return "", status_error.RuntimeConfigurationInvalid
	}
	info, err := os.Stat(canonical)
	if err != nil || !info.IsDir() {
		return "", status_error.RuntimeConfigurationInvalid
	}
	return canonical, nil
}

// readNativeAccountFile only reads the bounded regular file selected by its caller.
func readNativeAccountFile(path string) ([]byte, error) {
	const maxCredentialBytes = 1 << 20
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, status_error.RuntimeAuthRequired
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxCredentialBytes {
		return nil, status_error.AIAccountCredentialError
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, status_error.AIAccountCredentialError
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, status_error.AIAccountCredentialError
	}
	data, err := io.ReadAll(io.LimitReader(file, maxCredentialBytes+1))
	if err != nil || len(data) > maxCredentialBytes {
		clear(data)
		return nil, status_error.AIAccountCredentialError
	}
	return data, nil
}
