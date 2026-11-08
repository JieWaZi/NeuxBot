package installation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func managedFixture(t *testing.T) (*Installer, func(string) Result) {
	t.Helper()
	p := publisher(t)
	version := ""
	i := NewWithOptions(t.TempDir(), Options{OS: "darwin", Arch: "arm64", Client: p.client, Runner: fakeRunner{run: func(context.Context, Command) (string, error) { return "codex-cli " + version, nil }}})
	return i, func(next string) Result {
		t.Helper()
		version = next
		codexRelease(p, next, tarArtifact(t, map[string]string{"bin/codex": "owned executable " + next, "codex-path/rg": "owned companion"}, nil))
		result, err := i.Install(t.Context(), "codex", nil)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
}

func writeFixture(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestManagedUninstallRemovesEveryVersionAndPreservesAccounts(t *testing.T) {
	i, install := managedFixture(t)
	first := install("1.2.3")
	install("1.2.4")
	account := filepath.Join(i.root, "accounts", "codex", "auth.json")
	writeFixture(t, account, []byte("retained account fixture"))
	view, err := i.Inspect(t.Context(), "codex", first.Binary, nil)
	if err != nil || view.Method != "managed" || !view.CanUninstall || !view.Installed {
		t.Fatalf("managed capability = %+v error=%v", view, err)
	}
	if err := i.Uninstall(t.Context(), "codex", first.Binary, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := i.Installed("codex"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("uninstall fell back to another managed version: %v", err)
	}
	if data, err := os.ReadFile(account); err != nil || string(data) != "retained account fixture" {
		t.Fatal("uninstall modified an account")
	}
	entries, err := os.ReadDir(filepath.Join(i.root, "codex"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("program files remained: entries=%v error=%v", entries, err)
	}
}

func TestManagedUninstallValidatesAllReceiptsBeforeRemovingAnyVersion(t *testing.T) {
	i, install := managedFixture(t)
	first := install("1.2.3")
	latest := install("1.2.4")
	root := filepath.Dir(filepath.Dir(filepath.Dir(first.Binary)))
	writeFixture(t, filepath.Join(root, "installation.json"), []byte(`{"RuntimeID":"claude","Version":"1.2.3"}`))
	if err := i.Uninstall(t.Context(), "codex", latest.Binary, nil); err == nil {
		t.Fatal("tampered older version was silently removed")
	}
	if _, err := os.Stat(latest.Binary); err != nil {
		t.Fatal("validation failure removed a valid version")
	}
	if _, err := os.Lstat(filepath.Join(i.root, "codex", ".uninstall.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("validation failure created an uninstall owner")
	}
}

func TestOwnedManagedDamageCanPublishRepairWithoutTouchingUnknownOldBytes(t *testing.T) {
	for _, damage := range []string{"digest", "missing", "escape", "forged"} {
		t.Run(damage, func(t *testing.T) {
			i, install := managedFixture(t)
			old := install("1.2.3")
			root := filepath.Dir(filepath.Dir(filepath.Dir(old.Binary)))
			switch damage {
			case "digest":
				writeFixture(t, old.Binary, []byte("unrecognized bytes retained"))
			case "missing":
				if err := os.Remove(old.Binary); err != nil {
					t.Fatal(err)
				}
			case "escape":
				outside := filepath.Join(t.TempDir(), "outside")
				writeFixture(t, outside, []byte("not owned"))
				os.Remove(old.Binary)
				if err := os.Symlink(outside, old.Binary); err != nil {
					t.Fatal(err)
				}
			case "forged":
				writeFixture(t, filepath.Join(root, "installation.json"), []byte(`{"RuntimeID":"claude","Version":"1.2.3"}`))
			}
			view, err := i.Inspect(t.Context(), "codex", old.Binary, nil)
			if err != nil {
				t.Fatal(err)
			}
			if damage == "escape" || damage == "forged" {
				if view.CanInstall || view.CanUpdate || view.CanUninstall {
					t.Fatalf("unverified owner acquired repair: %+v", view)
				}
				return
			}
			if !view.CanUpdate || !view.CanUninstall || view.Reason != "repairable" {
				t.Fatalf("owned damage lost repair: %+v", view)
			}
			fixed := install("1.2.4")
			if fixed.Binary == old.Binary {
				t.Fatal("repair overwrote the damaged version")
			}
			if damage == "digest" {
				if data, _ := os.ReadFile(old.Binary); string(data) != "unrecognized bytes retained" {
					t.Fatal("repair deleted unknown old bytes")
				}
			}
			if err := i.Uninstall(t.Context(), "codex", fixed.Binary, nil); err != nil {
				t.Fatalf("repaired installation retained an undeletable damaged version: %v", err)
			}
			if _, err := i.Installed("codex"); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("damaged old version survived uninstall: %v", err)
			}
		})
	}
}

func TestSelectedManagedRepairDoesNotReuseAnotherHealthyCurrentRelease(t *testing.T) {
	i, install := managedFixture(t)
	selected := install("1.2.4")
	writeFixture(t, selected.Binary, []byte("damaged selected bytes"))
	healthy := install("1.2.4")
	var phases []string
	repaired, err := i.InstallAt(t.Context(), "codex", selected.Binary, nil, func(progress Progress) {
		phases = append(phases, progress.Phase)
	})
	if err != nil {
		t.Fatal(err)
	}
	if repaired.Binary == selected.Binary || repaired.Binary == healthy.Binary || repaired.Version != healthy.Version {
		t.Fatalf("repair reused an existing installation: selected=%+v healthy=%+v repaired=%+v", selected, healthy, repaired)
	}
	if !strings.Contains(strings.Join(phases, ","), "verifying,publishing") {
		t.Fatalf("repair did not freshly verify and publish: %v", phases)
	}
	if data, err := os.ReadFile(selected.Binary); err != nil || string(data) != "damaged selected bytes" {
		t.Fatal("repair changed the old selected program")
	}
	if _, err := os.Stat(healthy.Binary); err != nil {
		t.Fatal("repair changed the unrelated healthy installation")
	}
}

func TestManagedUninstallRemainsAvailableWithoutArtifactPlatformSupport(t *testing.T) {
	i, install := managedFixture(t)
	install("1.2.3")
	i.os = "unsupported-platform"
	view, err := i.Inspect(t.Context(), "codex", "", nil)
	if err != nil || !view.Installed || !view.CanUninstall || view.CanUpdate || view.CanInstall || view.Reason != "platform_unsupported" {
		t.Fatalf("unsupported platform lost filesystem owner: %+v %v", view, err)
	}
	if err := i.Uninstall(t.Context(), "codex", "", nil); err != nil {
		t.Fatal(err)
	}
}

func TestManagedStoreSymlinksCannotAcquireExternalDeletionOwnership(t *testing.T) {
	for _, level := range []string{"root", "base"} {
		t.Run(level, func(t *testing.T) {
			i, install := managedFixture(t)
			result := install("1.2.3")
			original := i.root
			if level == "base" {
				original = filepath.Join(i.root, "codex")
			}
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.Rename(original, outside); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, original); err != nil {
				t.Fatal(err)
			}
			view, err := i.Inspect(t.Context(), "codex", result.Binary, nil)
			if err != nil || view.CanUninstall || view.CanUpdate {
				t.Fatalf("symlink acquired ownership: %+v error=%v", view, err)
			}
			if err := i.Uninstall(t.Context(), "codex", result.Binary, nil); err == nil {
				t.Fatal("outside deletion was accepted")
			}
			if _, err := os.Stat(result.Binary); err != nil {
				t.Fatal("outside program was modified")
			}
		})
	}
}

func TestPartialUninstallRetainsDurableCleanupOwnerAcrossRestart(t *testing.T) {
	i, install := managedFixture(t)
	first := install("1.2.3")
	install("1.2.4")
	account := filepath.Join(i.root, "accounts", "auth.json")
	writeFixture(t, account, []byte("retained"))
	i.removeProgramDirectory = func(root string) error {
		_ = os.Remove(filepath.Join(root, "installation.json"))
		_ = os.Remove(filepath.Join(root, "payload", "bin", "codex"))
		return os.ErrPermission
	}
	if err := i.Uninstall(t.Context(), "codex", first.Binary, nil); err == nil {
		t.Fatal("partial file removal was reported as success")
	}
	restarted := NewWithOptions(i.root, Options{OS: "darwin", Arch: "arm64"})
	view, err := restarted.Inspect(t.Context(), "codex", first.Binary, nil)
	if err != nil || view.Installed || !view.CanUninstall || view.CanInstall || view.CanUpdate || view.Reason != "cleanup_pending" {
		t.Fatalf("cleanup owner was lost: %+v error=%v", view, err)
	}
	if err := restarted.Uninstall(t.Context(), "codex", first.Binary, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Installed("codex"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retry restored an old version: %v", err)
	}
	if data, err := os.ReadFile(account); err != nil || string(data) != "retained" {
		t.Fatal("cleanup retry modified an account")
	}
	if entries, err := os.ReadDir(filepath.Join(i.root, "codex")); err != nil || len(entries) != 0 {
		t.Fatalf("cleanup left owned files: %v %v", entries, err)
	}
}

func TestUnknownSelectedProgramNeverFallsBackToPrivateUpdate(t *testing.T) {
	i, install := managedFixture(t)
	private := install("1.2.3")
	external := filepath.Join(t.TempDir(), "bin", "codex")
	writeFixture(t, external, []byte("unknown system program"))
	view, err := i.Inspect(t.Context(), "codex", external, nil)
	if err != nil || !view.Installed || view.CanInstall || view.CanUpdate || view.CanUninstall || view.Method != "unknown" {
		t.Fatalf("unknown system ownership = %+v %v", view, err)
	}
	if _, err := i.InstallAt(t.Context(), "codex", external, nil, nil); err == nil {
		t.Fatal("system update silently installed a private copy")
	}
	if err := i.Uninstall(t.Context(), "codex", external, nil); err == nil {
		t.Fatal("unknown system file was removed")
	}
	current, err := i.Installed("codex")
	if err != nil || current.Binary != private.Binary {
		t.Fatal("unrelated private installation changed")
	}
	if data, _ := os.ReadFile(external); string(data) != "unknown system program" {
		t.Fatal("unknown program changed")
	}
}

func npmFixture(t *testing.T, windows bool) (string, string, string) {
	t.Helper()
	prefix := t.TempDir()
	module := filepath.Join(prefix, "lib", "node_modules", "@google", "gemini-cli")
	entry := filepath.Join(prefix, "bin", "gemini")
	if windows {
		module, entry = filepath.Join(prefix, "node_modules", "@google", "gemini-cli"), filepath.Join(prefix, "gemini.cmd")
	}
	manifest := filepath.Join(module, "package.json")
	writeFixture(t, manifest, []byte(`{"name":"@google/gemini-cli","version":"1.0.0","bin":{"gemini":"bundle/gemini.js"}}`))
	script := filepath.Join(module, "bundle", "gemini.js")
	writeFixture(t, script, []byte("#!/usr/bin/env node\n"))
	if windows {
		writeFixture(t, entry, []byte(`@node "%~dp0\node_modules\@google\gemini-cli\bundle\gemini.js" %*`))
	} else {
		if err := os.MkdirAll(filepath.Dir(entry), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(script, entry); err != nil {
			t.Fatal(err)
		}
	}
	return entry, manifest, script
}

func TestNPMUpdatesAndUninstallsTheOriginalGlobalPrefixWithoutReplacingShim(t *testing.T) {
	entry, manifest, script := npmFixture(t, false)
	var calls []Command
	i := NewWithOptions(t.TempDir(), Options{OS: "darwin", Arch: "arm64", LookPath: func(string) (string, error) { return "/trusted/npm", nil }, Runner: fakeRunner{run: func(_ context.Context, command Command) (string, error) {
		calls = append(calls, command)
		if command.Binary == "/trusted/npm" {
			if command.Args[0] == "uninstall" {
				if !containsString(command.Args, "--ignore-scripts") {
					t.Error("npm uninstall enabled package lifecycle hooks")
				}
				_ = os.RemoveAll(filepath.Dir(manifest))
				_ = os.Remove(entry)
			}
			return "", nil
		}
		if !samePath(resolvedFile(command.Binary), resolvedFile(script)) {
			t.Errorf("wrong version probe %s", command.Binary)
		}
		return "2.0.0", nil
	}}})
	before, _ := os.Readlink(entry)
	view, err := i.Inspect(t.Context(), "gemini", entry, nil)
	if err != nil || view.Method != "npm" || !view.CanUpdate || !view.CanUninstall {
		t.Fatalf("npm owner = %+v error=%v", view, err)
	}
	result, err := i.InstallAt(t.Context(), "gemini", entry, nil, nil)
	if err != nil || result.Version != "2.0.0" {
		t.Fatalf("npm update = %+v %v", result, err)
	}
	if after, _ := os.Readlink(entry); after != before {
		t.Fatal("installer rewrote the npm shim")
	}
	if len(calls) != 2 || !containsString(calls[0].Args, "--prefix") || !containsString(calls[0].Args, "@google/gemini-cli@latest") {
		t.Fatalf("wrong package command: %+v", calls)
	}
	if _, err := i.Installed("gemini"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("npm update created a private program copy")
	}
	if err := i.Uninstall(t.Context(), "gemini", result.Binary, result.PrefixArgs); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsNPMUsesNodeForBothManagerAndVersionProbe(t *testing.T) {
	entry, manifest, script := npmFixture(t, true)
	tools := t.TempDir()
	node, npm := filepath.Join(tools, "node.exe"), filepath.Join(tools, "npm.cmd")
	writeFixture(t, node, []byte("MZ fixture"))
	writeFixture(t, filepath.Join(tools, "node_modules", "npm", "bin", "npm-cli.js"), []byte("trusted npm fixture"))
	probes := 0
	i := NewWithOptions(t.TempDir(), Options{OS: "windows", Arch: "amd64", LookPath: func(name string) (string, error) {
		if name == "node" {
			return node, nil
		}
		return npm, nil
	}, Runner: fakeRunner{run: func(_ context.Context, command Command) (string, error) {
		if command.Binary != node {
			t.Fatalf("attempted to execute a Windows shim: %s", command.Binary)
		}
		if strings.HasSuffix(command.Args[0], "npm-cli.js") {
			return "", nil
		}
		if !samePath(command.Args[0], script) || command.Args[len(command.Args)-1] != "--version" {
			t.Fatalf("wrong node version probe: %#v", command.Args)
		}
		probes++
		return "2.0.0", nil
	}}})
	result, err := i.InstallAt(t.Context(), "gemini", entry, nil, nil)
	if err != nil || result.Binary != node || !reflect.DeepEqual(result.PrefixArgs, []string{script}) || probes != 1 {
		t.Fatalf("Windows package result=%+v probes=%d error=%v", result, probes, err)
	}
	if _, err := os.Stat(manifest); err != nil {
		t.Fatal("update removed npm metadata")
	}
}

func TestHomebrewUsesItsReceiptAndOriginalFormula(t *testing.T) {
	prefix := t.TempDir()
	program := filepath.Join(prefix, "Cellar", "gemini-cli", "1.0.0", "bin", "gemini")
	receipt := filepath.Join(prefix, "Cellar", "gemini-cli", "1.0.0", "INSTALL_RECEIPT.json")
	writeFixture(t, program, []byte("owned brew entry"))
	writeFixture(t, receipt, []byte(`{"source":{"tap":"homebrew/core"}}`))
	brew := filepath.Join(prefix, "bin", "brew")
	writeFixture(t, brew, []byte("trusted brew fixture"))
	entry := filepath.Join(prefix, "bin", "gemini")
	if err := os.Symlink(program, entry); err != nil {
		t.Fatal(err)
	}
	var invocation []string
	i := NewWithOptions(t.TempDir(), Options{OS: "darwin", Arch: "arm64", Runner: fakeRunner{run: func(_ context.Context, command Command) (string, error) {
		if samePath(resolvedFile(command.Binary), resolvedFile(brew)) {
			invocation = append([]string(nil), command.Args...)
			return "", nil
		}
		if !samePath(command.Binary, filepath.Join(resolvedFile(prefix), "bin", "gemini")) {
			t.Errorf("wrong brew version entry %s", command.Binary)
		}
		return "2.0.0", nil
	}}})
	view, err := i.Inspect(t.Context(), "gemini", entry, nil)
	if err != nil || view.Method != "homebrew" || !view.CanUpdate {
		t.Fatalf("brew owner=%+v error=%v", view, err)
	}
	if _, err := i.InstallAt(t.Context(), "gemini", entry, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(invocation, []string{"upgrade", "--formula", "gemini-cli"}) {
		t.Fatalf("wrong brew maintenance command: %v", invocation)
	}
	data, _ := json.Marshal(map[string]any{"source": map[string]any{"tap": "untrusted/tap"}})
	writeFixture(t, receipt, data)
	view, err = i.Inspect(t.Context(), "gemini", entry, nil)
	if err != nil || view.CanUpdate || view.CanUninstall {
		t.Fatal("untrusted Homebrew receipt was adopted")
	}
}

func TestProvenNativeUpdatesInPlaceWithoutPublishingAPrivateCopy(t *testing.T) {
	p := publisher(t)
	version := "1.0.0"
	oldBytes := append([]byte("\xcf\xfa\xed\xfe"), []byte("verified official native fixture 1")...)
	newBytes := append([]byte("\xcf\xfa\xed\xfe"), []byte("verified official native fixture 2")...)
	put := func(v string, binary []byte) {
		p.put("https://x.ai/cli/stable", []byte(v))
		p.put("https://x.ai/cli/grok-"+v+"-macos-aarch64", binary)
	}
	put(version, oldBytes)
	i := NewWithOptions(t.TempDir(), Options{OS: "darwin", Arch: "arm64", Client: p.client, Runner: fakeRunner{run: func(context.Context, Command) (string, error) { return "grok " + version, nil }}})
	private, err := i.Install(t.Context(), "grok", nil)
	if err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(t.TempDir(), "bin", "grok")
	writeFixture(t, selected, oldBytes)
	view, err := i.Inspect(t.Context(), "grok", selected, nil)
	if err != nil || view.Method != "native" || !view.CanUpdate || !view.CanUninstall {
		t.Fatalf("proven native owner=%+v error=%v", view, err)
	}
	// Wrong publisher version never reaches the selected native path.
	put("1.0.1", newBytes)
	if _, err := i.InstallAt(t.Context(), "grok", selected, nil, nil); err == nil {
		t.Fatal("wrong native receipt was published")
	}
	if data, _ := os.ReadFile(selected); !reflect.DeepEqual(data, oldBytes) {
		t.Fatal("failed version probe replaced native program")
	}
	version = "1.0.1"
	result, err := i.InstallAt(t.Context(), "grok", selected, nil, nil)
	if err != nil || !samePath(result.Binary, resolvedFile(selected)) || result.Version != version {
		t.Fatalf("native update=%+v error=%v", result, err)
	}
	if data, _ := os.ReadFile(selected); !reflect.DeepEqual(data, newBytes) {
		t.Fatal("selected native program was not updated")
	}
	stillPrivate, err := i.Installed("grok")
	if err != nil || stillPrivate.Binary != private.Binary || stillPrivate.Version != "1.0.0" {
		t.Fatal("native update substituted or published a private installation")
	}
	view, err = i.Inspect(t.Context(), "grok", selected, nil)
	if err != nil || view.Method != "native" || !view.CanUninstall {
		t.Fatal("new native ownership receipt was not retained")
	}
	if err := i.Uninstall(t.Context(), "grok", selected, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(selected); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("selected native program remained")
	}
	if _, err := os.Stat(private.Binary); err != nil {
		t.Fatal("native uninstall removed an unrelated private copy")
	}
}
