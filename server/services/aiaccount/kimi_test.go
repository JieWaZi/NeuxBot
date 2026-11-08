package aiaccount

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
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
	service.SetProgramResolver(func(context.Context, string) (string, []string, error) { return filepath.Join(bin, "kimi"), nil, nil })
	service.Start(context.Background())
	t.Cleanup(service.Close)
	for index, id := range []string{"0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210"} {
		account := models.AIAccount{ID: id, RuntimeID: constants.RuntimeIDKimi, Name: "Kimi " + string(rune('A'+index)), AuthMode: constants.AccountAuthModeSubscription,
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
	service.SetProgramResolver(func(context.Context, string) (string, []string, error) { return filepath.Join(bin, "kimi"), nil, nil })
	service.Start(context.Background())
	t.Cleanup(service.Close)
	id := "0123456789abcdef0123456789abcdef"
	account := models.AIAccount{ID: id, RuntimeID: constants.RuntimeIDKimi, Name: "Kimi", AuthMode: constants.AccountAuthModeSubscription,
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
	if release, err := service.BeginProgramMaintenance(t.Context(), constants.RuntimeIDKimi); err == nil {
		release()
		t.Fatal("program maintenance replaced a running Kimi login process")
	}
	service.CancelLogin(id, login.ID)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if release, err := service.Exclusive(id); err == nil {
			release()
			maintenance, err := service.BeginProgramMaintenance(t.Context(), constants.RuntimeIDKimi)
			if err != nil {
				t.Fatalf("confirmed native exit retained its program owner: %v", err)
			}
			maintenance()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("canceled native login did not release the account lease")
}

// TestKimiLoginUsesSelectedRuntimeProgram 验证 PATH 缺失或包含旧 Python CLI 时仍使用运行时确认的实际程序和前置参数。
func TestKimiLoginUsesSelectedRuntimeProgram(t *testing.T) {
	for _, oldOnPath := range []bool{false, true} {
		t.Run(map[bool]string{false: "PATH缺失", true: "PATH旧Python"}[oldOnPath], func(t *testing.T) {
			directory := t.TempDir()
			t.Setenv("PATH", directory)
			oldMarker := filepath.Join(directory, "old-cli-ran")
			if oldOnPath {
				script := "#!/bin/sh\nprintf old > '" + oldMarker + "'\nexit 91\n"
				if err := os.WriteFile(filepath.Join(directory, "kimi"), []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
			}
			selectedScript := filepath.Join(directory, "selected-2.5.2.sh")
			script := "#!/bin/sh\n[ \"$1\" = login ] || exit 88\n[ \"$KIMI_SHARE_DIR\" = \"$KIMI_CODE_HOME\" ] || exit 89\nprintf 'Opening browser for Kimi device login: https://www.kimi.com/code/authorize_device?user_code=TRUE-2520\\n'\nprintf 'default_model = \"kimi-code/k3\"\\n' > \"$KIMI_CODE_HOME/config.toml\"\n"
			if err := os.WriteFile(selectedScript, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "accounts.db"), Migrate: true})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, _ := database.DB.DB()
			t.Cleanup(func() { _ = sqlDB.Close() })
			accounts := controllers.NewAIAccountController(database.DB)
			service := New(accounts, t.TempDir())
			service.Start(t.Context())
			t.Cleanup(service.Close)
			calls := 0
			service.SetProgramResolver(func(_ context.Context, runtimeID string) (string, []string, error) {
				calls++
				if runtimeID != "kimi-managed-profile" {
					t.Errorf("resolver used wrong runtime: %s", runtimeID)
				}
				return "/bin/sh", []string{selectedScript}, nil
			})
			id := "0123456789abcdef0123456789abcdef"
			account := models.AIAccount{ID: id, RuntimeID: "kimi-managed-profile", Name: "Selected Kimi", AuthMode: constants.AccountAuthModeSubscription, Endpoints: []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolOpenAICompletions, BaseURL: constants.AIAccountKimiURL}}}
			if err := accounts.Create(t.Context(), account, ""); err != nil {
				t.Fatal(err)
			}
			login, err := service.StartLogin(t.Context(), id)
			if err != nil || login.UserCode != "TRUE-2520" || calls != 1 {
				t.Fatalf("selected native login=%+v err=%v resolver calls=%d", login, err, calls)
			}
			deadline := time.After(3 * time.Second)
			for !login.Complete {
				select {
				case <-deadline:
					t.Fatal("selected runtime login did not commit")
				case <-time.After(time.Millisecond):
				}
				login, err = service.GetLogin(t.Context(), id, login.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := os.Stat(oldMarker); !os.IsNotExist(err) {
				t.Fatal("managed login invoked stale CLI from PATH")
			}
		})
	}
}
