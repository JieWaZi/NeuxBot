package capability

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestCodexShellConnectionMaintainsNativeOwnership(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	contextRoot := filepath.Join(root, constants.AgentCLIContextDirectory)
	name := constants.AgentCLIBinaryName
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(root, constants.AgentCLISystemSkillsDirectory, strings.Repeat("a", 64), constants.AgentCLISkillScriptsDirectory, name)
	if err := os.MkdirAll(filepath.Dir(binary), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("bundled entry fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	r := New(nil)
	t.Cleanup(r.Close)
	endpoint := "http://127.0.0.1:18080" + constants.AgentCLIPath
	prepare := func(chat, execution string) (string, func()) {
		t.Helper()
		file, activate, finish, err := r.PrepareConnection(contextRoot, chat, endpoint, GrantSpec{
			ExecutionID: execution,
			Validate: func(context.Context) error {
				return nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(finish)
		activate()
		return file, finish
	}
	firstID, otherID := "01900000-0000-7000-8000-000000000001", "01900000-0000-7000-8000-000000000002"
	file, finish := prepare("chat", "first")
	if err := r.BindCodexShellConnection(file, "unadmitted", firstID); !errors.Is(err, status_error.AgentContextUnauthorized) {
		t.Fatalf("different execution bound native thread: %v", err)
	}
	if err := r.BindCodexShellConnection(file, "first", "../other"); !errors.Is(err, status_error.AgentContextStorageError) {
		t.Fatalf("invalid native thread ID was accepted: %v", err)
	}
	if err := r.BindCodexShellConnection(file, "first", firstID); err != nil {
		t.Fatal(err)
	}
	initial, err := ReadConnection(file)
	resolved, aliasErr := ReadCodexShellConnection(binary, firstID)
	if err != nil || aliasErr != nil || initial != resolved {
		t.Fatalf("native lookup selected a different context: %v %v", err, aliasErr)
	}
	if _, err := ReadCodexShellConnection(file, firstID); !errors.Is(err, status_error.AgentContextStorageError) {
		t.Fatalf("unbundled executable was accepted: %v", err)
	}
	alias, _ := codexShellConnectionPath(contextRoot, firstID)
	stable, err := os.Stat(alias)
	if err != nil {
		t.Fatal(err)
	}
	finish()
	reused, finishSecond := prepare("chat", "second")
	if err := r.BindCodexShellConnection(reused, "second", firstID); err != nil {
		t.Fatal(err)
	}
	if same, err := os.Stat(alias); err != nil || !os.SameFile(stable, same) {
		t.Fatalf("hot continuation rewrote the native lookup: %v", err)
	}
	other, _ := prepare("other-chat", "other")
	if err := r.BindCodexShellConnection(other, "other", firstID); !errors.Is(err, status_error.AgentContextUnauthorized) {
		t.Fatalf("independent execution stole an active native lookup: %v", err)
	}
	finishSecond()
	renewed, _ := prepare("chat", "replacement")
	if err := r.BindCodexShellConnection(renewed, "replacement", otherID); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCodexShellConnection(binary, firstID); !errors.Is(err, status_error.AgentContextStorageError) {
		t.Fatalf("replaced native thread retained its lookup: %v", err)
	}
	if resolved, err := ReadCodexShellConnection(binary, otherID); err != nil || resolved != initial {
		t.Fatalf("replacement native thread lost its resident credential: %v", err)
	}
	r.Close()
	if _, err := ReadCodexShellConnection(binary, otherID); !errors.Is(err, status_error.AgentContextStorageError) {
		t.Fatalf("Core close did not remove its native lookup: %v", err)
	}
}

func TestCleanupRetiredConnectionsKeepsCurrentAndUnknownFiles(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registry := New(nil)
	identity := strings.Repeat("a", 64)
	retiredEpoch := strings.Repeat("b", 32)
	otherEpoch := strings.Repeat("c", 32)
	threadID := "01900000-0000-7000-8000-000000000001"
	descriptor := Connection{
		ProtocolVersion: constants.AgentCLIProtocolVersion,
		Endpoint:        "http://127.0.0.1:18080" + constants.AgentCLIPath,
		GrantID:         "test-grant",
		Token:           "test-token",
	}
	fixtures := []struct {
		directory string // Relative managed descriptor directory.
		epoch     string // Core epoch recorded in the descriptor.
		remove    bool   // Whether the descriptor belongs to a retired Core.
	}{
		{identity, retiredEpoch, true},
		{filepath.Join(retiredEpoch, identity), retiredEpoch, true},
		{filepath.Join(codexShellDirectory, threadID), retiredEpoch, true},
		{filepath.Join(registry.epoch, identity), registry.epoch, false},
		{filepath.Join(otherEpoch, identity), retiredEpoch, false},
		{filepath.Join(otherEpoch, "unmanaged"), otherEpoch, false},
	}
	for _, fixture := range fixtures {
		descriptor.CoreEpoch = fixture.epoch
		if _, err := WriteConnection(filepath.Join(root, fixture.directory), residentConnectionFilename, descriptor); err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.CleanupRetiredConnections(root); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		directory := filepath.Join(root, fixture.directory)
		if fixture.remove {
			if _, err := os.Stat(directory); !os.IsNotExist(err) {
				t.Errorf("retired connection directory %s remains: %v", fixture.directory, err)
			}
		} else if _, err := ReadConnection(filepath.Join(directory, residentConnectionFilename)); err != nil {
			t.Errorf("connection outside retirement scope %s changed: %v", fixture.directory, err)
		}
	}
}
