package accountconfig

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

type claudeMemoryKeychain struct {
	// values 将每个精确配置根的原生凭据相互隔离。
	values map[string][]byte
	// calls 记录是否越过了文件系统边界校验。
	calls int
	// writeErr 模拟系统 Keychain 容量不足。
	writeErr error
	// removeErr 模拟旧精确项无法安全移除。
	removeErr error
}

func (k *claudeMemoryKeychain) Read(_ context.Context, home string) ([]byte, error) {
	k.calls++
	value, ok := k.values[home]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), value...), nil
}
func (k *claudeMemoryKeychain) Write(_ context.Context, home string, data []byte) error {
	k.calls++
	if k.writeErr != nil {
		return k.writeErr
	}
	k.values[home] = append([]byte(nil), data...)
	return nil
}
func (k *claudeMemoryKeychain) Remove(_ context.Context, home string) error {
	k.calls++
	if k.removeErr != nil {
		return k.removeErr
	}
	delete(k.values, home)
	return nil
}

func claudeSnapshotFixture() ClaudeCredentialSnapshot {
	return ClaudeCredentialSnapshot{AccountID: "account-a", OrganizationID: "org-a", Credential: []byte(`{"claudeAiOauth":{"accessToken":"fixture-access","refreshToken":"fixture-refresh","expiresAt":123}}`), Metadata: []byte(`{"oauthAccount":{"accountUuid":"account-a","organizationUuid":"org-a","emailAddress":"fixture@example.test"}}`)}
}
func TestClaudeCredentialSnapshotIsolatedFileAndKeychain(t *testing.T) {
	for _, platform := range []string{"linux", "darwin"} {
		t.Run(platform, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "managed")
			first, second := filepath.Join(root, "first", "1"), filepath.Join(root, "second", "1")
			for _, home := range []string{first, second} {
				if err := os.MkdirAll(home, 0700); err != nil {
					t.Fatal(err)
				}
			}
			root, _ = filepath.EvalSymlinks(root)
			first, second = filepath.Join(root, "first", "1"), filepath.Join(root, "second", "1")
			keychain := &claudeMemoryKeychain{values: map[string][]byte{}}
			store := NewClaudeCredentialStore(root, keychain)
			store.platform = platform
			snapshot := claudeSnapshotFixture()
			if err := store.Write(t.Context(), first, snapshot); err != nil {
				t.Fatal(err)
			}
			if err := store.Write(t.Context(), second, snapshot); err != nil {
				t.Fatal(err)
			}
			read, err := store.Read(t.Context(), first)
			if err != nil || string(read.Credential) != string(snapshot.Credential) || string(read.Metadata) != string(snapshot.Metadata) {
				t.Fatalf("native snapshot did not round trip: %v", err)
			}
			if err := store.Remove(t.Context(), first); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Read(t.Context(), second); err != nil {
				t.Fatal("another managed account was affected", err)
			}
			if platform == "darwin" {
				if _, err := os.Stat(filepath.Join(second, constants.ClaudeCredentialsFileName)); !os.IsNotExist(err) {
					t.Fatal("keychain credential leaked to an ordinary file")
				}
			}
		})
	}
}
func TestClaudeCredentialStoreRejectsSystemAndSymlinkHomes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "managed")
	home := filepath.Join(root, "account", "1")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	root, _ = filepath.EvalSymlinks(root)
	home = filepath.Join(root, "account", "1")
	system := t.TempDir()
	marker := filepath.Join(system, constants.ClaudeCredentialsFileName)
	if err := os.WriteFile(marker, []byte(`{"system":"untouched"}`), 0600); err != nil {
		t.Fatal(err)
	}
	keychain := &claudeMemoryKeychain{values: map[string][]byte{}}
	store := NewClaudeCredentialStore(root, keychain)
	store.platform = "darwin"
	for _, action := range []func() error{func() error { _, err := store.Read(t.Context(), system); return err }, func() error { return store.Write(t.Context(), system, claudeSnapshotFixture()) }, func() error { return store.Remove(t.Context(), system) }} {
		if err := action(); !errors.Is(err, status_error.AIAccountConfigurationInvalid) {
			t.Fatalf("system root accepted: %v", err)
		}
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(system, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	if err := store.Write(t.Context(), link, claudeSnapshotFixture()); !errors.Is(err, status_error.AIAccountWorkspaceError) {
		t.Fatalf("symlink home accepted: %v", err)
	}
	if keychain.calls != 0 {
		t.Fatal("boundary failure touched keychain")
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != `{"system":"untouched"}` {
		t.Fatal("system credential changed")
	}
}

func TestClaudeCredentialKeychainFallbackRequiresOldItemRemoval(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	home := filepath.Join(root, "account", "1")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	keychain := &claudeMemoryKeychain{values: map[string][]byte{home: []byte(`{"old":"fixture"}`)}, writeErr: status_error.AIAccountCredentialError, removeErr: status_error.AIAccountCredentialError}
	store := NewClaudeCredentialStore(root, keychain)
	store.platform = "darwin"
	if err := store.Write(t.Context(), home, claudeSnapshotFixture()); err == nil {
		t.Fatal("unsafe file fallback left old keychain item authoritative")
	}
	if _, err := os.Stat(filepath.Join(home, constants.ClaudeCredentialsFileName)); !os.IsNotExist(err) {
		t.Fatal("file fallback ran before old item removal")
	}
	keychain.removeErr = nil
	if err := store.Write(t.Context(), home, claudeSnapshotFixture()); err != nil {
		t.Fatal(err)
	}
	read, err := store.Read(t.Context(), home)
	if err != nil || string(read.Credential) != string(claudeSnapshotFixture().Credential) {
		t.Fatal("native file fallback failed", err)
	}
	info, err := os.Stat(filepath.Join(home, constants.ClaudeCredentialsFileName))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("file fallback was not private")
	}
}
