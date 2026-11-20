//go:build darwin || linux

package cliinstall

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBashFindsExecutableInNewLoginAndNonLoginTerminals(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is unavailable")
	}
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(bin, "botcli")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	login := filepath.Join(home, ".bash_login")
	original := "export PATH=/usr/bin:/bin\nexport ORIGINAL=kept\n"
	writeFixture(t, login, original)
	for range 2 {
		if err := configureShellPath(home, "bash"); err != nil {
			t.Fatal(err)
		}
	}
	if got := readFixture(t, login); !strings.HasPrefix(got, original) || strings.Count(got, "# NeuxBot CLI") != 1 {
		t.Fatalf("login profile was overwritten or duplicated: %q", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".bash_profile")); !os.IsNotExist(err) {
		t.Fatalf("installer masked the existing login profile: %v", err)
	}
	for _, flags := range []string{"-lic", "-ic"} {
		command := exec.Command(bash, flags, `command -v botcli; printf '%s\n' "$PATH"`)
		command.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin", "TERM=dumb"}
		if flags == "-lic" {
			// System /etc/profile can override HOME in test runners. Source the
			// selected login file with a clean HOME to isolate the user startup.
			command = exec.Command(bash, "--noprofile", "-lic", `. "$HOME/.bash_login"; command -v botcli; printf '%s\n' "$PATH"`)
			command.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin", "TERM=dumb"}
		}
		output, err := command.Output()
		if err != nil || !strings.HasPrefix(string(output), executable+"\n") || strings.Count(string(output), bin) != 2 {
			t.Fatalf("new bash %s could not resolve a single installed CLI path: %q (%v)", flags, output, err)
		}
	}
}

func TestZshPreservesSymlinkedProfile(t *testing.T) {
	config := t.TempDir()
	t.Setenv("ZDOTDIR", config)
	profile := filepath.Join(config, ".zshrc")
	target := filepath.Join(t.TempDir(), "zshrc")
	writeFixture(t, target, "export CUSTOM='dotfiles'\n")
	if err := os.Symlink(target, profile); err != nil {
		t.Fatal(err)
	}
	if err := configureShellPath(t.TempDir(), "zsh"); err != nil {
		t.Fatal(err)
	}
	if link, err := os.Readlink(profile); link != target || err != nil {
		t.Fatalf("symlink replaced: %q, %v", link, err)
	}
	if got := readFixture(t, target); !strings.HasPrefix(got, "export CUSTOM='dotfiles'\n") || !strings.Contains(got, ".local/bin") {
		t.Fatalf("linked config not updated in place: %q", got)
	}
}

func TestFishUsesConfiguredHome(t *testing.T) {
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	home := t.TempDir()
	if err := configureShellPath(home, "fish"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(config, "fish", "config.fish")); err != nil {
		t.Fatalf("installer ignored XDG_CONFIG_HOME: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config")); !os.IsNotExist(err) {
		t.Fatalf("installer wrote to default config when XDG_CONFIG_HOME is set: %v", err)
	}
}
