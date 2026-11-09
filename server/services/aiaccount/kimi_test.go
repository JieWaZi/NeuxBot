package aiaccount

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestKimiDeviceOutputWaitsForCompleteURL(t *testing.T) {
	output := &kimiLoginOutput{ready: make(chan string, 1)}
	_, _ = output.Write([]byte("Opening browser for Kimi device login: https://www.kimi.com/code/authorize_device?user_code=AB"))
	select {
	case <-output.ready:
		t.Fatal("accepted a partial device code")
	default:
	}
	_, _ = output.Write([]byte("CD-EFGH\n"))
	select {
	case address := <-output.ready:
		if !strings.HasSuffix(address, "ABCD-EFGH") {
			t.Fatal("device code was truncated")
		}
	default:
		t.Fatal("complete device URL was not detected")
	}
}

func TestKimiSubscriptionUsesIndependentNativeHomes(t *testing.T) {
	dataRoot := t.TempDir()
	bin := t.TempDir()
	script := "#!/bin/sh\nprintf 'Opening browser for Kimi device login: https://www.kimi.com/code/authorize_device?user_code=ABCD-EFGH\\n'\nprintf 'default_model = \"kimi-code/k3\"\\n[models.\"kimi-code/k3\"]\\nprovider = \"managed:kimi-code\"\\nmodel = \"k3\"\\nmax_context_size = 1048576\\n' > \"$KIMI_CODE_HOME/config.toml\"\n"
	if err := os.WriteFile(filepath.Join(bin, "kimi"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "accounts.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	accounts := controllers.NewAIAccountController(database.DB)
	service := New(accounts, dataRoot)
	service.Start(context.Background())
	t.Cleanup(service.Close)
	for index, id := range []string{"0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210"} {
		account := models.AIAccount{ID: id, Name: "Kimi " + string(rune('A'+index)), AuthMode: constants.AccountAuthModeSubscription,
			Endpoint: models.AIAccountEndpoint{Protocol: constants.AccountProtocolOpenAICompletions, BaseURL: constants.AIAccountKimiURL}}
		if err := accounts.Create(context.Background(), account, ""); err != nil {
			t.Fatal(err)
		}
		login, err := service.StartLogin(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if login.UserCode != "ABCD-EFGH" {
			t.Fatal("native device code was not returned")
		}
		// 原生退出与凭据落库异步完成，不能用固定 200ms 约束满载构建机。
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			login, err = service.GetLogin(context.Background(), id, login.ID)
			if err != nil {
				t.Fatal(err)
			}
			if login.Complete {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !login.Complete {
			t.Fatal("native login was not committed")
		}

		loggedIn, err := accounts.Get(t.Context(), id)
		if err != nil || loggedIn.AuthGeneration != 2 {
			t.Fatalf("native login did not rotate its unverified identity generation: %+v err=%v", loggedIn, err)
		}
		request := agent.AccountRequest{
			Provider: constants.RuntimeIDKimi, AccountID: id, CWD: t.TempDir(), Model: "kimi-code/k3",
		}
		selected, err := service.ResolveAccount(t.Context(), request, "")
		if err != nil {
			t.Fatal(err)
		}
		lease, err := service.AcquireAccount(t.Context(), selected)
		if err != nil {
			t.Fatal(err)
		}
		defer lease.Close(t.Context())
		home, err := filepath.EvalSymlinks(filepath.Join(dataRoot, "ai-accounts", "kimi", id))
		if err != nil {
			t.Fatal(err)
		}
		if selected.ConfigHome != home || lease.ConfigHome != home || lease.Environment["KIMI_CODE_HOME"] != home ||
			lease.Environment["KIMI_SHARE_DIR"] != home {
			t.Fatal("subscription account reused another native home")
		}
		if err := lease.Close(t.Context()); err != nil {
			t.Fatal(err)
		}

		if err := accounts.StoreLoginCredential(t.Context(), id, loggedIn.Revision, kimiCredentialMarker); err != nil {
			t.Fatal(err)
		}
		reauthenticated, err := service.ResolveAccount(t.Context(), request, "")
		if err != nil || reauthenticated.ConfigHome != selected.ConfigHome || reauthenticated.Subject == selected.Subject {
			t.Fatalf("unverified reauthentication reused session subject: before=%+v after=%+v err=%v", selected, reauthenticated, err)
		}
	}
}

func TestCancelKimiLoginReleasesAccountAfterNativeExit(t *testing.T) {
	bin := t.TempDir()
	script := "#!/bin/sh\nprintf 'Opening browser for Kimi device login: https://www.kimi.com/code/authorize_device?user_code=ABCD-EFGH\\n'\nexec sleep 30\n"
	if err := os.WriteFile(filepath.Join(bin, "kimi"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "accounts.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	accounts := controllers.NewAIAccountController(database.DB)
	service := New(accounts, t.TempDir())
	service.Start(context.Background())
	t.Cleanup(service.Close)
	id := "0123456789abcdef0123456789abcdef"
	account := models.AIAccount{ID: id, Name: "Kimi", AuthMode: constants.AccountAuthModeSubscription,
		Endpoint: models.AIAccountEndpoint{Protocol: constants.AccountProtocolOpenAICompletions, BaseURL: constants.AIAccountKimiURL}}
	if err := accounts.Create(context.Background(), account, ""); err != nil {
		t.Fatal(err)
	}
	login, err := service.StartLogin(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if release, err := service.Exclusive(id); err == nil {
		release()
		t.Fatal("native login did not hold the account lease")
	}
	service.CancelLogin(id, login.ID)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if release, err := service.Exclusive(id); err == nil {
			release()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("canceled native login did not release the account lease")
}

func TestKimiLoginCommitsNativeIdentityAcrossConcurrentMetadataSave(t *testing.T) {
	service, _, _ := subscriptionFixture(t)
	service.dataDirectory = t.TempDir()
	service.kimiDataDirectory = filepath.Join(service.dataDirectory, "ai-accounts", "kimi")
	account := models.AIAccount{ID: "fedcba9876543210fedcba9876543210", Name: "before", AuthMode: constants.AccountAuthModeSubscription, Endpoint: models.AIAccountEndpoint{Protocol: constants.AccountProtocolOpenAICompletions, BaseURL: constants.AIAccountKimiURL}}
	if err := service.accounts.Create(t.Context(), account, kimiCredentialMarker); err != nil {
		t.Fatal(err)
	}
	home, err := service.kimiHome(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("old native subject"), 0600); err != nil {
		t.Fatal(err)
	}
	original, err := service.ResolveAccount(t.Context(), agent.AccountRequest{Provider: "kimi", AccountID: account.ID, CWD: t.TempDir()}, "")
	if err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
printf 'new native subject' > "$KIMI_CODE_HOME/config.toml"
printf 'Opening browser for Kimi device login: https://www.kimi.com/code/authorize_device?user_code=ABCD-EFGH\n'
while [ ! -f "$KIMI_CODE_HOME/finish" ]; do sleep 0.01; done
`
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "kimi"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	invalidated := make(chan uint64, 1)
	service.SetAccountAuthInvalidator(func(_ context.Context, provider, id string, generation uint64) {
		if provider == "kimi" && id == account.ID {
			invalidated <- generation
		}
	})
	login, err := service.StartLogin(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	current, err := service.accounts.Get(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	current.Name = "renamed during native login"
	current.DefaultModel = "new-default"
	current.DefaultReasoning = "high"
	if err := service.accounts.Update(t.Context(), current, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "finish"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		login, err = service.GetLogin(t.Context(), account.ID, login.ID)
		if err != nil {
			t.Fatal(err)
		}
		if login.Complete || login.ErrorCode != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	saved, err := service.accounts.Get(t.Context(), account.ID)
	if err != nil || !login.Complete || login.ErrorCode != "" || saved.AuthGeneration != 2 || saved.Name != current.Name || saved.DefaultModel != "new-default" || saved.DefaultReasoning != "high" {
		t.Fatalf("native identity commit lost metadata or generation: login=%+v account=%+v err=%v", login, saved, err)
	}
	select {
	case generation := <-invalidated:
		if generation != 1 {
			t.Fatalf("invalidated generation %d", generation)
		}
	default:
		t.Fatal("old catalog generation was not invalidated")
	}
	next, err := service.ResolveAccount(t.Context(), original, "")
	if err != nil || next.Subject == original.Subject {
		t.Fatalf("changed native login reused old subject: %+v err=%v", next, err)
	}
	if _, err := service.AcquireAccount(t.Context(), original); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("old credential result remained admissible: %v", err)
	}
	if err := service.accounts.StoreRefreshedCredential(t.Context(), account.ID, saved.Revision, original.AuthGeneration, kimiCredentialMarker); !errors.Is(err, status_error.AIAccountRevisionConflict) {
		t.Fatalf("old generation callback accepted: %v", err)
	}
}
