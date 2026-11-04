package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

const (
	CatalogFresh       = "fresh"
	CatalogStale       = "stale"
	CatalogUnavailable = "unavailable"
	CatalogSignedOut   = "signed_out"
)

// ModelCatalogSnapshot carries discovery evidence for one account and installation.
type ModelCatalogSnapshot struct {
	// AgentModelCatalog retains the native order and per-model settings.
	AgentModelCatalog
	// Capabilities contains the initialized native client's evidence.
	Capabilities AgentCapabilities
	// State distinguishes a fresh empty response, stale evidence, and sign-out.
	State string
	// DetectedAt is the last successful native read in Unix milliseconds.
	DetectedAt int64
	// ErrorCode is a safe explanation for a failed refresh.
	ErrorCode string
}

// ModelCatalog owns model discovery and short-lived snapshots. ProviderRuntime
// retains process ownership even if a query is cancelled or cleanup must retry.
type ModelCatalog struct {
	// providers opens the same credential-bound native clients used for execution.
	providers *ProviderRuntime
	// mu guards identity changes, in-flight queries, and snapshot publication.
	mu sync.Mutex
	// entries keeps one snapshot per account and runtime option.
	entries map[modelCatalogSlot]*modelCatalogEntry
}

type modelCatalogSlot struct {
	// runtime is the configured runtime option.
	runtime string
	// account identifies the provider account whose models were observed.
	account providerAccount
}

type modelCatalogEntry struct {
	// key includes the credential subject, generation, and native launch identity.
	key [sha256.Size]byte
	// generation is the exact account authorization bound to this snapshot.
	generation uint64
	// result is the previous complete response or its safe failure.
	result ModelCatalogSnapshot
	// pending closes after the in-flight read has settled.
	pending chan struct{}
}

// NewModelCatalog builds the catalog using the application process owner.
func NewModelCatalog(providers *ProviderRuntime) *ModelCatalog {
	return &ModelCatalog{providers: providers, entries: make(map[modelCatalogSlot]*modelCatalogEntry)}
}

// InvalidateAccount discards results and prevents late reads from publishing for the revoked generation.
func (c *ModelCatalog) InvalidateAccount(accountID string, throughGeneration uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for slot, entry := range c.entries {
		if slot.account.id == accountID && entry.generation <= throughGeneration {
			delete(c.entries, slot)
		}
	}
}

// ReadForRuntime refreshes through a management connection without opening a native thread.
func (c *ModelCatalog) ReadForRuntime(ctx context.Context, runtimeID string, request ProviderConnectionRequest, version, accountModelFingerprint string, refresh bool) ModelCatalogSnapshot {
	if c == nil || c.providers == nil || request.Account.Subject == "" {
		return modelCatalogFailure(status_error.RuntimeUnavailable)
	}
	slot := modelCatalogSlot{runtime: runtimeID, account: providerAccount{provider: request.Account.Provider, id: request.Account.AccountID}}
	key := modelCatalogKey(request, version, accountModelFingerprint)
	c.mu.Lock()
	entry := c.entries[slot]
	if entry == nil || entry.key != key {
		entry = &modelCatalogEntry{key: key, generation: request.Account.AuthGeneration}
		c.entries[slot] = entry
	}
	if entry.pending != nil {
		pending := entry.pending
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return modelCatalogFailure(ctx.Err())
		case <-pending:
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.entries[slot] != entry {
				return modelCatalogFailure(status_error.RuntimeUnavailable)
			}
			return cloneModelCatalog(entry.result)
		}
	}
	if !refresh && entry.result.State == CatalogFresh && time.Since(time.UnixMilli(entry.result.DetectedAt)) < 5*time.Minute {
		defer c.mu.Unlock()
		return cloneModelCatalog(entry.result)
	}
	entry.pending = make(chan struct{})
	c.mu.Unlock()

	result := c.readNative(ctx, request)
	c.mu.Lock()
	defer c.mu.Unlock()
	close(entry.pending)
	entry.pending = nil
	if c.entries[slot] != entry {
		return modelCatalogFailure(status_error.RuntimeUnavailable)
	}
	if result.State == CatalogUnavailable && entry.result.DetectedAt != 0 {
		previous := entry.result
		previous.State, previous.ErrorCode = CatalogStale, result.ErrorCode
		result = previous
	}
	entry.result = result
	return cloneModelCatalog(result)
}

func modelCatalogKey(request ProviderConnectionRequest, version, modelFingerprint string) [sha256.Size]byte {
	account := request.Account
	account.CWD, account.Model = "", ""
	launch := request.Launch
	launch.BaseEnvironment = nil
	executable := launch.Executable
	if path, err := exec.LookPath(executable); err == nil {
		executable = path
	}
	var executableStamp []int64
	if stat, err := os.Stat(executable); err == nil {
		executableStamp = []int64{stat.ModTime().UnixNano(), stat.Size()}
	}
	var configStamp []int64
	if account.AccountID == SystemAccountID {
		if stat, err := os.Stat(filepath.Join(account.ConfigHome, "config.toml")); err == nil {
			configStamp = []int64{stat.ModTime().UnixNano(), stat.Size()}
		}
	}
	encoded, _ := json.Marshal([]any{account, launch, executable, executableStamp, configStamp, version, modelFingerprint})
	return sha256.Sum256(encoded)
}

func (c *ModelCatalog) readNative(ctx context.Context, request ProviderConnectionRequest) (snapshot ModelCatalogSnapshot) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	conn, err := c.providers.Open(ctx, request, ProviderConnectionPorts{})
	if conn != nil {
		defer func() {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.providers.options.CleanupTimeout)
			defer cancel()
			if err := conn.Close(cleanup); err != nil && snapshot.State == CatalogFresh {
				snapshot = modelCatalogFailure(err)
			}
		}()
	}
	if err != nil {
		return modelCatalogFailure(err)
	}
	client, err := conn.Client()
	if err != nil {
		return modelCatalogFailure(err)
	}
	capabilities := client.Capabilities()
	fail := func(err error) ModelCatalogSnapshot {
		failure := modelCatalogFailure(err)
		failure.Capabilities = capabilities
		return failure
	}
	account, err := client.ReadAccount(ctx)
	if err != nil {
		return fail(err)
	}
	if account.RequiresAuthentication && account.Account == nil {
		return fail(status_error.RuntimeAuthRequired)
	}
	models, err := client.ReadModels(ctx)
	if err != nil {
		return fail(err)
	}
	if models.Models == nil {
		return fail(status_error.AIAccountModelsUnavailable)
	}
	return ModelCatalogSnapshot{AgentModelCatalog: models, Capabilities: client.Capabilities(), State: CatalogFresh, DetectedAt: time.Now().UnixMilli()}
}

func modelCatalogFailure(err error) ModelCatalogSnapshot {
	switch {
	case errors.Is(err, status_error.RuntimeAuthRequired):
		return ModelCatalogSnapshot{State: CatalogSignedOut, ErrorCode: "sign_in_required"}
	case errors.Is(err, context.DeadlineExceeded):
		return ModelCatalogSnapshot{State: CatalogUnavailable, ErrorCode: "timeout"}
	case errors.Is(err, context.Canceled):
		return ModelCatalogSnapshot{State: CatalogUnavailable, ErrorCode: "cancelled"}
	default:
		return ModelCatalogSnapshot{State: CatalogUnavailable, ErrorCode: "catalog_unavailable"}
	}
}

func cloneModelCatalog(snapshot ModelCatalogSnapshot) ModelCatalogSnapshot {
	snapshot.Models = cloneChoices(snapshot.Models)
	return snapshot
}
