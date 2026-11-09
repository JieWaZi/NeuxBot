package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestAIAccountCredentialsAndRevision(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "accounts.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	controller := NewAIAccountController(database.DB)
	ctx := context.Background()
	account := models.AIAccount{ID: "0123456789abcdef0123456789abcdef", Name: "Kimi", AuthMode: constants.AccountAuthModeAPIKey, Endpoint: models.AIAccountEndpoint{Protocol: constants.AccountProtocolAnthropicMessages, BaseURL: "https://api.kimi.com/coding"}}
	if err := controller.Create(ctx, account, "secret-for-test"); err != nil {
		t.Fatal(err)
	}
	var stored models.AIAccount
	if err := database.DB.First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.CredentialCiphertext == "" || strings.Contains(stored.CredentialCiphertext, "secret-for-test") {
		t.Fatal("credential must be encrypted")
	}
	listed, err := controller.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(listed)
	if strings.Contains(string(data), "secret-for-test") || strings.Contains(string(data), stored.CredentialCiphertext) {
		t.Fatal("list exposed credentials")
	}
	current, err := controller.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	current.Name = "Renamed"
	if err := controller.Update(ctx, current, ""); err != nil {
		t.Fatal(err)
	}
	secret, err := controller.Credential(ctx, current.ID)
	if err != nil || secret != "secret-for-test" {
		t.Fatalf("empty update did not preserve key: %v", err)
	}
	if err := controller.Update(ctx, current, "replacement"); !errors.Is(err, status_error.AIAccountRevisionConflict) {
		t.Fatalf("stale update accepted: %v", err)
	}
	current, err = controller.Get(ctx, current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Endpoint != account.Endpoint {
		t.Fatal("endpoint was not retained")
	}
	if err := controller.Update(ctx, current, "replacement"); err != nil {
		t.Fatal(err)
	}
	secret, err = controller.Credential(ctx, current.ID)
	if err != nil || secret != "replacement" {
		t.Fatalf("replacement missing: %v", err)
	}
	// 把账号 A 的密文移给账号 B，附加认证数据必须阻止身份串用。
	other := account
	other.ID = "abcdef0123456789abcdef0123456789"
	if err := controller.Create(ctx, other, "other-secret"); err != nil {
		t.Fatal(err)
	}
	if err := database.DB.Model(&models.AIAccount{}).Where("id = ?", other.ID).Update("credential_ciphertext", stored.CredentialCiphertext).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Credential(ctx, other.ID); !errors.Is(err, status_error.AIAccountCredentialError) {
		t.Fatal("ciphertext copied across identities")
	}
}

func TestAIAccountNamesAreUnique(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "accounts.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	controller := NewAIAccountController(database.DB)
	ctx := context.Background()
	first := models.AIAccount{ID: "first", Name: "OpenAI", AuthMode: constants.AccountAuthModeAPIKey}
	if err := controller.Create(ctx, first, "first-key"); err != nil {
		t.Fatal(err)
	}
	second := models.AIAccount{ID: "second", Name: first.Name, AuthMode: constants.AccountAuthModeAPIKey}
	if err := controller.Create(ctx, second, "second-key"); !errors.Is(err, status_error.AIAccountNameConflict) {
		t.Fatalf("duplicate account name accepted: %v", err)
	}
	second.Name = "DeepSeek"
	if err := controller.Create(ctx, second, "second-key"); err != nil {
		t.Fatal(err)
	}
	second, err = controller.Get(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	second.Name = first.Name
	if err := controller.Update(ctx, second, "replacement-key"); !errors.Is(err, status_error.AIAccountNameConflict) {
		t.Fatalf("duplicate rename accepted: %v", err)
	}
	unchanged, err := controller.Get(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Name != "DeepSeek" || unchanged.Revision != second.Revision {
		t.Fatal("failed rename changed account metadata")
	}
	secret, err := controller.Credential(ctx, second.ID)
	if err != nil || secret != "second-key" {
		t.Fatalf("failed rename changed account credential: %v", err)
	}
	if err := controller.Update(ctx, unchanged, ""); err != nil {
		t.Fatalf("keeping the same account name should succeed: %v", err)
	}
}

func TestAIAccountAuthGenerationTracksCredentialBoundaries(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "accounts.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	repo := NewAIAccountController(database.DB)
	ctx := t.Context()
	account := models.AIAccount{
		ID: "0123456789abcdef0123456789abcdef", Name: "account", AuthMode: constants.AccountAuthModeAPIKey,
		Endpoint: models.AIAccountEndpoint{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: constants.AIAccountOpenAIURL},
	}
	if err := repo.Create(ctx, account, "first"); err != nil {
		t.Fatal(err)
	}
	read := func(expected uint64) models.AIAccount {
		t.Helper()
		actual, err := repo.Get(ctx, account.ID)
		if err != nil || actual.AuthGeneration != expected {
			t.Fatalf("auth generation = %d, expected %d: %v", actual.AuthGeneration, expected, err)
		}
		return actual
	}
	current := read(1)
	current.Name = "renamed"
	current.SelectedModels = []string{"selected"}
	if err := repo.Update(ctx, current, ""); err != nil {
		t.Fatal(err)
	}
	current = read(1)
	if err := repo.StoreCredential(ctx, current.ID, current.Revision, "refreshed"); err != nil {
		t.Fatal(err)
	}
	current = read(1)
	if err := repo.Update(ctx, current, "replacement"); err != nil {
		t.Fatal(err)
	}
	current = read(2)
	current.Endpoint = models.AIAccountEndpoint{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: "https://different.example/v1"}
	if err := repo.Update(ctx, current, ""); err != nil {
		t.Fatal(err)
	}
	current = read(3)
	if err := repo.ClearCredential(ctx, current.ID, current.Revision); err != nil {
		t.Fatal(err)
	}
	current = read(4)
	if current.HasCredential {
		t.Fatal("logout left the credential marked available")
	}
	if secret, err := repo.Credential(ctx, current.ID); err != nil || secret != "" {
		t.Fatal("logout left a decryptable credential")
	}
	if err := repo.StoreLoginCredential(ctx, current.ID, current.Revision, "new-login"); err != nil {
		t.Fatal(err)
	}
	if current = read(5); !current.HasCredential {
		t.Fatal("login did not persist the replacement")
	}
	if err := repo.ClearCredential(ctx, current.ID, current.Revision-1); !errors.Is(err, status_error.AIAccountRevisionConflict) {
		t.Fatalf("stale logout accepted: %v", err)
	}
	read(5)
}

func TestAIAccountAuthGenerationMigratesExistingRows(t *testing.T) {
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "accounts.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	repo := NewAIAccountController(db.DB)
	account := models.AIAccount{ID: "old-account", Name: "Existing account", AuthMode: constants.AccountAuthModeAPIKey}
	if err := repo.Create(t.Context(), account, "existing-key"); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Exec("ALTER TABLE ai_accounts DROP COLUMN auth_generation").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.AutoMigrate(&models.AIAccount{}); err != nil {
		t.Fatal(err)
	}
	current, err := repo.Get(t.Context(), account.ID)
	if err != nil || current.AuthGeneration != 1 || current.Revision != 1 || !current.HasCredential {
		t.Fatalf("existing account did not retain its baseline: %+v %v", current, err)
	}
	if secret, err := repo.Credential(t.Context(), account.ID); err != nil || secret != "existing-key" {
		t.Fatal("migration lost the encrypted credential")
	}
}
