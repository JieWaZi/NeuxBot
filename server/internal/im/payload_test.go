package im

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestFrozenFileRejectsRebindingAnAuthorizedRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("此用例要求本机可创建目录符号链接")
	}
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	approved := filepath.Join(parent, "approved")
	if err := os.Mkdir(approved, 0o700); err != nil {
		t.Fatal(err)
	}
	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("outside grant"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(approved, approved+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, approved); err != nil {
		t.Fatal(err)
	}
	if _, err := FreezeFile(t.TempDir(), "delivery", 0, filepath.Join(approved, "secret.txt"), []string{approved}); !errors.Is(err, status_error.IMFileInvalid) {
		t.Fatalf("rebound grant root was accepted: %v", err)
	}
}
