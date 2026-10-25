package aiaccount

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
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
			Endpoints: []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolOpenAICompletions, BaseURL: constants.AIAccountKimiURL}}}
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
		for attempt := 0; attempt < 20; attempt++ {
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
		target, release, err := service.Prepare(context.Background(), id, constants.RuntimeIDKimi, t.TempDir(), "kimi-code/k3")
		if err != nil {
			t.Fatal(err)
		}
		if target.Environment["KIMI_CODE_HOME"] != filepath.Join(dataRoot, "ai-accounts", "kimi", id) {
			t.Fatal("subscription account reused another native home")
		}
		release()
		models, defaultModel, err := service.KimiSubscriptionModels(context.Background(), id)
		if err != nil || len(models) != 1 || models[0] != "kimi-code/k3" || defaultModel != "kimi-code/k3" {
			t.Fatalf("native Kimi models were not linked to account: %v %v %v", models, defaultModel, err)
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
		Endpoints: []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolOpenAICompletions, BaseURL: constants.AIAccountKimiURL}}}
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
