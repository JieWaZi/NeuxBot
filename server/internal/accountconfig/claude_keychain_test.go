package accountconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestClaudeKeychainDirectoryIdentityAndSecretTransport(t *testing.T) {
	home := t.TempDir()
	credential := []byte(`{"claudeAiOauth":{"accessToken":"fixture-secret"}}`)
	digest := sha256.Sum256([]byte(home))
	service := "Claude Code-credentials-" + hex.EncodeToString(digest[:4])
	calls := 0
	keychain := &claudeNativeKeychain{username: func() (string, error) { return "fixture-user", nil }, run: func(_ context.Context, args []string, input []byte) ([]byte, error) {
		calls++
		for _, arg := range args {
			if strings.Contains(arg, "fixture-secret") || strings.Contains(arg, hex.EncodeToString(credential)) {
				t.Fatal("credential appeared in process arguments")
			}
		}
		switch calls {
		case 1:
			if !reflect.DeepEqual(args, []string{"-i"}) || !strings.Contains(string(input), `-s "`+service+`"`) || !strings.Contains(string(input), hex.EncodeToString(credential)) {
				t.Fatalf("unexpected private Keychain write: %v", args)
			}
		case 2:
			if !reflect.DeepEqual(args, []string{"find-generic-password", "-a", "fixture-user", "-w", "-s", service}) {
				t.Fatalf("wrong exact Keychain item: %v", args)
			}
			return append([]byte(nil), credential...), nil
		case 3:
			if !reflect.DeepEqual(args, []string{"delete-generic-password", "-a", "fixture-user", "-s", service}) {
				t.Fatalf("wrong deletion scope: %v", args)
			}
		}
		return nil, nil
	}}
	if err := keychain.Write(t.Context(), home, credential); err != nil {
		t.Fatal(err)
	}
	actual, err := keychain.Read(t.Context(), home)
	if err != nil || string(actual) != string(credential) {
		t.Fatalf("credential round trip failed: %v", err)
	}
	if err := keychain.Remove(t.Context(), home); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("unexpected native calls: %d", calls)
	}
}

func TestClaudeKeychainRejectsUnsafeOrOversizeInputWithoutSystemAccess(t *testing.T) {
	keychain := &claudeNativeKeychain{username: func() (string, error) { return "fixture-user", nil }, run: func(context.Context, []string, []byte) ([]byte, error) {
		t.Fatal("invalid input reached system Keychain")
		return nil, nil
	}}
	if _, err := keychain.Read(t.Context(), ""); !errors.Is(err, status_error.AIAccountConfigurationInvalid) {
		t.Fatal("unset directory must not select the system item")
	}
	if err := keychain.Write(t.Context(), t.TempDir(), []byte(`{"opaque":"`+strings.Repeat("a", 3000)+`"}`)); !errors.Is(err, status_error.AIAccountCredentialError) {
		t.Fatal("oversize input must use the managed file fallback")
	}
	keychain.run = func(context.Context, []string, []byte) ([]byte, error) { return nil, os.ErrNotExist }
	if _, err := keychain.Read(t.Context(), t.TempDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing exact item must allow file fallback")
	}
}

func TestClaudeKeychainPreservesNativeUnicodeAndExplicitPathIdentity(t *testing.T) {
	root := t.TempDir()
	a, err := claudeKeychainService(filepath.Join(root, "caf\u00e9", "account"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := claudeKeychainService(filepath.Join(root, "cafe\u0301", "account"))
	if err != nil || a != b {
		t.Fatal("native NFC identity changed")
	}
	other, err := claudeKeychainService(filepath.Join(root, "caf\u00e9", "account") + string(filepath.Separator))
	if err != nil || other == a {
		t.Fatal("explicit CLI config string was silently cleaned")
	}
}
