package agent

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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

func TestGeminiCatalogUsesNativeDataLeaseWithoutAccountEndpoint(t *testing.T) {
	request := providerTestRequest(t, "gemini")
	request.Account.DataHome = request.Account.CWD
	dataHome := request.Account.DataHome
	runtime := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: func(context.Context, AccountRequest) (AccountLease, error) {
		return AccountLease{DataHome: dataHome}, nil
	}})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	catalog := NewModelCatalog(runtime)
	result := catalog.ReadForRuntime(t.Context(), "gemini", request, "", "", false)
	if result.State != CatalogFresh || result.DefaultID != "fixture-gemini-model" {
		t.Fatalf("native session did not supply the catalog: %+v", result)
	}
	dataHome = filepath.Join(dataHome, "other")
	if _, err := runtime.Open(t.Context(), request, ProviderConnectionPorts{}); err != status_error.AIAccountConfigurationInvalid {
		t.Fatalf("a lease for a different data root was admitted: %v", err)
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

func TestQoderCatalogReusesVerifiedLoginAndHonorsNativeSignOut(t *testing.T) {
	for _, mode := range []string{"available", "signed-out"} {
		t.Run(mode, func(t *testing.T) {
			request := providerTestRequest(t, "qoder")
			request.Launch.Environment["FIXTURE_QODER_MODELS"] = mode
			runtime := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: func(context.Context, AccountRequest) (AccountLease, error) {
				return AccountLease{QoderUserID: "fixture-native-user", QoderLoginVerified: true}, nil
			}})
			t.Cleanup(func() { _ = runtime.Close(context.Background()) })
			result := NewModelCatalog(runtime).ReadForRuntime(t.Context(), "qoder", request, "", "", false)
			if mode == "available" {
				if result.State != CatalogFresh || !result.QoderLoginVerified || result.DefaultID != "fixture-qoder-model" || len(result.Models) != 3 {
					t.Fatalf("verified login did not produce the native model catalog: %+v", result)
				}
				for _, id := range []string{"fixture-qoder-model", "fixture-acp-only", "fixture-qoder-auto"} {
					if !slices.ContainsFunc(result.Models, func(row AgentChoice) bool { return row.ID == id && row.Group == "" }) {
						t.Fatalf("ACP model missing or host grouping retained: %s %+v", id, result.Models)
					}
				}
			} else if result.State != CatalogSignedOut || result.QoderLoginVerified || len(result.Models) != 0 {
				t.Fatalf("native sign-out did not invalidate earlier login evidence: %+v", result)
			}
			trace, err := os.ReadFile(filepath.Join(request.Account.CWD, "trace"))
			if err != nil || !strings.Contains(string(trace), "session/new") || strings.Contains(string(trace), "--print") {
				t.Fatalf("unexpected Qoder management launches: %s (%v)", trace, err)
			}
			if strings.Contains(string(trace), "daemon.get_models") {
				t.Fatalf("Qoder discovery still queried SDK models: %s", trace)
			}
		})
	}
}

func TestQoderCatalogCanCloseWhileNativeReadIsPending(t *testing.T) {
	request := providerTestRequest(t, "qoder")
	request.Launch.Environment["FIXTURE_QODER_MODELS"] = "pending"
	runtime := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: func(context.Context, AccountRequest) (AccountLease, error) {
		return AccountLease{QoderLoginVerified: true}, nil
	}})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	catalog := NewModelCatalog(runtime)
	read := make(chan ModelCatalogSnapshot, 1)
	go func() { read <- catalog.ReadForRuntime(ctx, "qoder", request, "", "", false) }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	pending := filepath.Join(request.Account.CWD, "qoder-catalog-pending")
	for {
		if _, err := os.Stat(pending); err == nil {
			break
		}
		select {
		case <-ticker.C:
		case result := <-read:
			t.Fatalf("model read returned before reaching native request: %+v", result)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if err := runtime.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-read:
		if result.State != CatalogUnavailable || len(result.Models) != 0 {
			t.Fatalf("shutdown published an incomplete catalog: %+v", result)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
