package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestCodexCatalogUsesManagementOnlyAndPartitionsAccounts(t *testing.T) {
	request := providerTestRequest(t, "codex")
	acquisitions := 0
	runtime := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: func(context.Context, AccountRequest) (AccountLease, error) {
		acquisitions++
		return AccountLease{}, nil
	}})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	catalog := NewModelCatalog(runtime)
	a := catalog.ReadForRuntime(t.Context(), "codex", request, "", "", false)
	if a.State != CatalogFresh || a.Models == nil {
		t.Fatalf("catalog = %+v", a)
	}
	if cached := catalog.ReadForRuntime(t.Context(), "codex", request, "", "", false); cached.State != CatalogFresh || acquisitions != 1 {
		t.Fatalf("cache = %+v, acquisitions = %d", cached, acquisitions)
	}
	request.Account.AccountID = "another"
	request.Account.Subject = "another"
	if b := catalog.ReadForRuntime(t.Context(), "codex", request, "", "", false); b.State != CatalogFresh || acquisitions != 2 {
		t.Fatalf("other account = %+v, acquisitions = %d", b, acquisitions)
	}
	trace, err := os.ReadFile(filepath.Join(request.Account.CWD, "trace"))
	if err != nil || !strings.Contains(string(trace), "model/list") || strings.Contains(string(trace), "thread/start") || strings.Contains(string(trace), "turn/start") {
		t.Fatalf("management trace = %s, %v", trace, err)
	}
}

func TestCodexCatalogKeepsOnlySameIdentityStaleAndClearsOnSignOut(t *testing.T) {
	request := providerTestRequest(t, "codex")
	var failure error
	malformed := false
	runtime := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: func(context.Context, AccountRequest) (AccountLease, error) {
		if malformed {
			return AccountLease{Environment: map[string]string{"FIXTURE_CODEX_MALFORMED_MODELS": "true"}}, nil
		}
		return AccountLease{}, failure
	}})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	catalog := NewModelCatalog(runtime)
	read := func(refresh bool) ModelCatalogSnapshot {
		return catalog.ReadForRuntime(t.Context(), "codex", request, "", "", refresh)
	}
	fresh := read(false)
	if fresh.State != CatalogFresh || len(fresh.Models) == 0 {
		t.Fatalf("catalog = %+v", fresh)
	}
	malformed = true
	stale := read(true)
	if stale.State != CatalogStale || stale.DetectedAt != fresh.DetectedAt || len(stale.Models) != len(fresh.Models) {
		t.Fatalf("refresh erased prior evidence: %+v", stale)
	}
	malformed = false
	failure = status_error.RuntimeAuthRequired
	if signedOut := read(true); signedOut.State != CatalogSignedOut || signedOut.Models != nil {
		t.Fatalf("signed-out cache exposed models: %+v", signedOut)
	}
	failure = status_error.RuntimeUnavailable
	request.Account.AuthGeneration++
	if next := read(false); next.State != CatalogUnavailable || next.Models != nil {
		t.Fatalf("new auth generation borrowed catalog: %+v", next)
	}
}
