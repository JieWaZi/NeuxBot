package terminals

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTerminalRootUsesFrozenCanonicalGrant(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(allowed, "outside")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if root, err := authorizedRoot(allowed, []string{allowed}); err != nil || root == "" {
		t.Fatal(err)
	}
	for _, root := range []string{outside, link, "../relative"} {
		if _, err := authorizedRoot(root, []string{allowed}); err == nil {
			t.Fatalf("accepted unauthorized root %s", root)
		}
	}
}
