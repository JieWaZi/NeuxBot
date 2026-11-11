package aiaccount

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// closeAccountLease 在测试中核验账号回收结果，避免忽略可重试关闭失败。
func closeAccountLease(t *testing.T, release func(context.Context) error) {
	t.Helper()
	if err := release(context.Background()); err != nil {
		t.Error(err)
	}
}

func TestManagedCodexAccountsIsolateHumansInSameWorkspace(t *testing.T) {
	s, first, original := subscriptionFixture(t)
	s.dataDirectory = t.TempDir()
	second := first
	second.ID, second.Name = "fedcba9876543210fedcba9876543210", "second"
	other := codexCredential{Tokens: subscriptionTokens("other-human", "other-refresh"), LastRefresh: time.Now().UTC()}
	if err := s.accounts.Create(t.Context(), second, mustEncodeCredential(t, other)); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	hostHome := t.TempDir()
	t.Setenv("CODEX_HOME", hostHome)
	type result struct {
		// request holds the frozen native launch inputs.
		request agent.AccountRequest
		// lease keeps the selected auth generation active.
		lease agent.AccountLease
		// err reports any admission or acquisition failure.
		err error
	}
	var acquired [2]result
	var workers sync.WaitGroup
	for i, id := range []string{first.ID, second.ID} {
		workers.Add(1)
		go func() {
			defer workers.Done()
			r := &acquired[i]
			r.request, r.err = s.ResolveCodexAccount(t.Context(), agent.AccountRequest{
				Provider: constants.RuntimeIDCodex, AccountID: id, CWD: workspace,
			}, hostHome)
			if r.err == nil {
				r.lease, r.err = s.AcquireCodexAccount(t.Context(), r.request)
			}
		}()
	}
	workers.Wait()
	for _, r := range acquired {
		if r.err != nil {
			t.Fatal(r.err)
		}
		t.Cleanup(func() { closeAccountLease(t, r.lease.Close) })
		if r.request.ConfigHome == hostHome || r.lease.ConfigHome != r.request.ConfigHome || r.request.Subject == "" {
			t.Fatal("managed admission did not bind its isolated native home")
		}
	}
	a, b := acquired[0], acquired[1]
	if a.request.ConfigHome == b.request.ConfigHome || a.request.Subject == b.request.Subject {
		t.Fatal("different humans in one workspace shared native identity")
	}
	proof := sha256.Sum256([]byte(original.Tokens.AccessToken))
	if err := a.lease.VerifyCodexCredential(t.Context(), proof); err != nil {
		t.Fatal(err)
	}
	if err := b.lease.VerifyCodexCredential(t.Context(), proof); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("another human's process was accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(hostHome, "auth.json")); !os.IsNotExist(err) {
		t.Fatalf("managed launch changed system auth: %v", err)
	}
}

func TestManagedCodexAPIKeyGenerationFencesStaleSelection(t *testing.T) {
	s, _, _ := subscriptionFixture(t)
	s.dataDirectory = t.TempDir()
	ctx := t.Context()
	account := models.AIAccount{
		ID: "fedcba9876543210fedcba9876543210", Name: "key", AuthMode: constants.AccountAuthModeAPIKey,
		Endpoint:       models.AIAccountEndpoint{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: constants.AIAccountOpenAIURL},
		SelectedModels: []string{"fixture-model"},
	}
	if err := s.accounts.Create(ctx, account, "fixture-first-key"); err != nil {
		t.Fatal(err)
	}
	input := agent.AccountRequest{Provider: constants.RuntimeIDCodex, AccountID: account.ID, CWD: t.TempDir()}
	first, err := s.ResolveCodexAccount(ctx, input, "")
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.accounts.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	current.Name = "renamed"
	current.ModelDisplayNames = map[string]string{"fixture-model": "Visible model"}
	if err := s.accounts.Update(ctx, current, ""); err != nil {
		t.Fatal(err)
	}
	next, err := s.ResolveCodexAccount(ctx, input, "")
	if err != nil || next.Subject != first.Subject || next.AuthGeneration != first.AuthGeneration || next.ConfigHome != first.ConfigHome || next.ConfigFingerprint == first.ConfigFingerprint {
		t.Fatalf("model metadata did not refresh launch identity independently of auth/history: %v", err)
	}
	lease, err := s.AcquireCodexAccount(ctx, next)
	if err != nil {
		t.Fatal(err)
	}
	if lease.CodexCredentialKind != "api_key" {
		t.Fatal("native startup did not require an API key account")
	}
	if err := lease.VerifyCodexCredential(ctx, sha256.Sum256([]byte("fixture-first-key"))); err != nil {
		t.Fatalf("native proof for the selected key was rejected: %v", err)
	}
	if err := lease.VerifyCodexCredential(ctx, sha256.Sum256([]byte("other-key"))); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("unselected API key was accepted: %v", err)
	}
	closeAccountLease(t, lease.Close)
	paths, err := filepath.Glob(filepath.Join(first.ConfigHome, "models-*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatal("selected account model catalog missing")
	}
	if data, err := os.ReadFile(paths[0]); err != nil || len(data) == 0 {
		t.Fatal("selected account models were not materialized")
	}
	current, err = s.accounts.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.accounts.Update(ctx, current, "fixture-second-key"); err != nil {
		t.Fatal(err)
	}
	next, err = s.ResolveCodexAccount(ctx, input, "")
	if err != nil || next.AuthGeneration <= first.AuthGeneration || next.ConfigHome == first.ConfigHome {
		t.Fatalf("replacement did not fence the previous native generation: %v", err)
	}
	if _, err := s.AcquireCodexAccount(ctx, first); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("stale selection acquired: %v", err)
	}
}

func TestManagedCodexExclusiveClosesIdleWithoutAdmittingAnotherOwner(t *testing.T) {
	s, account, _ := subscriptionFixture(t)
	s.dataDirectory = t.TempDir()
	request, err := s.ResolveCodexAccount(t.Context(), agent.AccountRequest{
		Provider: constants.RuntimeIDCodex, AccountID: account.ID, CWD: t.TempDir(),
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := s.AcquireCodexAccount(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	s.SetIdleSessionCloser(func(ctx context.Context, id string) error {
		if id != account.ID {
			t.Fatal("exclusive change asked to close another account")
		}
		if next, err := s.AcquireCodexAccount(ctx, request); !errors.Is(err, status_error.AIAccountBusy) {
			t.Errorf("native admission was open during account mutation: %v", err)
			if next.Close != nil {
				closeAccountLease(t, next.Close)
			}
		}
		return lease.Close(ctx)
	})
	release, err := s.Exclusive(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

type codexRefreshRepo struct {
	// AIAccountController owns the real fixture database.
	controllers.AIAccountController
	// fail simulates an unsuccessful CAS once while saving native refresh.
	fail bool
}

func (r *codexRefreshRepo) StoreRefreshedCredential(ctx context.Context, id string, revision int64, generation uint64, secret string) error {
	if r.fail {
		r.fail = false
		return status_error.AIAccountRevisionConflict
	}
	return r.AIAccountController.StoreRefreshedCredential(ctx, id, revision, generation, secret)
}

func TestManagedCodexRefreshKeepsIdentityAndRetriesFailedRelease(t *testing.T) {
	s, account, original := subscriptionFixture(t)
	s.dataDirectory = t.TempDir()
	s.accounts = &codexRefreshRepo{AIAccountController: s.accounts, fail: true}
	ctx := t.Context()
	input := agent.AccountRequest{Provider: constants.RuntimeIDCodex, AccountID: account.ID, CWD: t.TempDir()}
	first, err := s.ResolveCodexAccount(ctx, input, "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := s.AcquireCodexAccount(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	next := original
	next.Tokens.AccessToken += "-refreshed"
	next.Tokens.RefreshToken = "native-rotated"
	next.LastRefresh = time.Now().UTC()
	if err := writeCodexChatGPTCredential(first.ConfigHome, next); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(ctx); !errors.Is(err, status_error.AIAccountRevisionConflict) {
		t.Fatalf("release hid native refresh failure: %v", err)
	}
	if _, err := s.Exclusive(account.ID); !errors.Is(err, status_error.AIAccountBusy) {
		t.Fatalf("release failure abandoned its lease: %v", err)
	}
	closeAccountLease(t, lease.Close)
	closeAccountLease(t, lease.Close)
	if saved, err := s.accounts.Credential(ctx, account.ID); err != nil || saved != mustEncodeCredential(t, next) {
		t.Fatal("native rotation was not saved after retry")
	}
	resolved, err := s.ResolveCodexAccount(ctx, input, "")
	if err != nil || !resolved.Equal(first) {
		t.Fatalf("token rotation changed the frozen identity: %v", err)
	}
	again, err := s.AcquireCodexAccount(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	defer closeAccountLease(t, again.Close)
	if err := again.VerifyCodexCredential(ctx, sha256.Sum256([]byte(next.Tokens.AccessToken))); err != nil {
		t.Fatal("reopened lease rejected the rotated native identity")
	}
}

func TestManagedCodexIdleRefreshPreservesConfigurationCAS(t *testing.T) {
	s, account, original := subscriptionFixture(t)
	s.dataDirectory = t.TempDir()
	ctx := t.Context()
	request, err := s.ResolveCodexAccount(ctx, agent.AccountRequest{
		Provider: constants.RuntimeIDCodex, AccountID: account.ID, CWD: t.TempDir(),
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := s.AcquireCodexAccount(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	refreshed := original
	refreshed.Tokens.RefreshToken = "rotated-during-idle"
	refreshed.LastRefresh = time.Now().UTC()
	if err := writeCodexChatGPTCredential(lease.ConfigHome, refreshed); err != nil {
		t.Fatal(err)
	}
	s.SetIdleSessionCloser(func(ctx context.Context, _ string) error { return lease.Close(ctx) })
	release, err := s.Exclusive(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if saved, err := s.accounts.Credential(ctx, account.ID); err != nil || saved != mustEncodeCredential(t, refreshed) {
		t.Fatal("idle close did not save the native rotation")
	}
	if err := s.accounts.ClearCredential(ctx, account.ID, account.Revision); err != nil {
		t.Fatalf("idle refresh invalidated the user's configuration revision: %v", err)
	}
}

func TestManagedCodexInvalidProjectionAllowsCredentialRepair(t *testing.T) {
	for _, wrongPrincipal := range []bool{false, true} {
		name := "malformed"
		if wrongPrincipal {
			name = "wrong_principal"
		}
		t.Run(name, func(t *testing.T) {
			s, account, original := subscriptionFixture(t)
			s.dataDirectory = t.TempDir()
			ctx := t.Context()
			request, err := s.ResolveCodexAccount(ctx, agent.AccountRequest{
				Provider: constants.RuntimeIDCodex, AccountID: account.ID, CWD: t.TempDir(),
			}, "")
			if err != nil {
				t.Fatal(err)
			}
			lease, err := s.AcquireCodexAccount(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			if wrongPrincipal {
				foreign := codexCredential{Tokens: subscriptionTokens("other", "wrong-refresh"), LastRefresh: time.Now().UTC()}
				err = writeCodexChatGPTCredential(lease.ConfigHome, foreign)
			} else {
				err = accountconfig.WriteFile(filepath.Join(lease.ConfigHome, "auth.json"), []byte("{}"))
			}
			if err != nil {
				t.Fatal(err)
			}
			s.SetIdleSessionCloser(func(ctx context.Context, _ string) error { return lease.Close(ctx) })
			release, err := s.Exclusive(account.ID)
			if err != nil {
				t.Fatalf("invalid projection trapped the native lease: %v", err)
			}
			defer release()
			if saved, err := s.accounts.Credential(ctx, account.ID); err != nil || saved != mustEncodeCredential(t, original) {
				t.Fatal("invalid projection replaced the saved credential")
			}
			if err := s.RemoveCodexCredentialFiles(ctx, account); err != nil {
				t.Fatal(err)
			}
			if err := s.accounts.ClearCredential(ctx, account.ID, account.Revision); err != nil {
				t.Fatal(err)
			}
		})
	}
}
