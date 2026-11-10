package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

func TestTargetCacheHitDoesNotRequireWritableStaging(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows directory permissions do not enforce a read-only staging parent")
	}
	root, cache := t.TempDir(), t.TempDir()
	standardInput(t, root, map[string]any{"name": "cache-hit"})
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	target, err := PrepareNativeTarget(t.Context(), cache, "p", pkg, root, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cache, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cache, 0700) })
	again, err := PrepareNativeTarget(t.Context(), cache, "p", pkg, root, "claude")
	if err != nil || again.Root != target.Root || again.Digest != target.Digest {
		t.Fatalf("verified cache hit tried to regenerate its tree: target=%+v error=%v", again, err)
	}
}

func TestTargetCacheGenerationInputsCannotReuseAnotherManifest(t *testing.T) {
	root, cache := t.TempDir(), t.TempDir()
	standardInput(t, root, map[string]any{"name": "manifest-input", "description": "Original"})
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := PrepareNativeTarget(t.Context(), cache, "p", pkg, root, "claude")
	if err != nil {
		t.Fatal(err)
	}
	pkg.Manifest.Description = "Changed"
	second, err := PrepareNativeTarget(t.Context(), cache, "p", pkg, root, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if second.Root == first.Root || second.Digest == first.Digest {
		t.Fatal("a different accepted manifest reused the previous generated target")
	}
	data, err := os.ReadFile(filepath.Join(second.Root, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if json.Unmarshal(data, &manifest) != nil || manifest["description"] != "Changed" {
		t.Fatal("generated target lost the accepted manifest", string(data))
	}
}

func TestTargetPublicationLockHonorsCancellation(t *testing.T) {
	root, cache := t.TempDir(), t.TempDir()
	standardInput(t, root, map[string]any{"name": "cancel-publication"})
	digest, err := ContentDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(t.TempDir(), "first")
	if err := CopyDeliveryTree(root, stage); err != nil {
		t.Fatal(err)
	}
	if _, err := publishTarget(t.Context(), cache, stage, "p", digest, "claude", digest); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(cache, ".locks"))
	if err != nil || len(entries) != 1 {
		t.Fatal("missing publication lock", err)
	}
	lock := flock.New(filepath.Join(cache, ".locks", entries[0].Name()))
	if err := lock.Lock(); err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	stage = filepath.Join(t.TempDir(), "cancelled")
	if err := CopyDeliveryTree(root, stage); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	completed := make(chan error, 1)
	go func() {
		_, err := publishTarget(ctx, cache, stage, "p", digest, "claude", digest)
		completed <- err
	}()
	select {
	case err := <-completed:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("publication did not preserve cancellation: %v", err)
		}
	case <-time.After(time.Second):
		_ = lock.Close()
		<-completed
		t.Fatal("publication waited beyond its cancelled context")
	}
}

func TestTargetCacheFailedRepairKeepsPreviouslyReturnedRoot(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "failed-repair"})
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	cache := t.TempDir()
	target, err := PrepareNativeTarget(t.Context(), cache, "p", pkg, root, "claude")
	if err != nil {
		t.Fatal(err)
	}
	fixtureFile(t, target.Root, "damage.txt", "damaged")
	if _, err := publishTarget(t.Context(), cache, filepath.Join(t.TempDir(), "missing"), "p", target.SourceDigest, "claude", target.Digest); err == nil {
		t.Fatal("missing repair stage accepted")
	}
	if _, err := os.Stat(filepath.Join(target.Root, "damage.txt")); err != nil {
		t.Fatal("failed repair deleted previously returned path", err)
	}
}

func TestTargetCacheTwoProcessesPublishOneImmutableRepair(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "concurrent-repair"})
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	cache := t.TempDir()
	target, err := PrepareNativeTarget(t.Context(), cache, "p", pkg, root, "claude")
	if err != nil {
		t.Fatal(err)
	}
	fixtureFile(t, target.Root, "damage.txt", "damaged")
	var wait sync.WaitGroup
	results := make([]string, 2)
	failures := make([]error, 2)
	stop := make(chan struct{})
	readDone := make(chan bool)
	go func() {
		intact := true
		for {
			select {
			case <-stop:
				readDone <- intact
				return
			default:
				if _, err := os.Stat(target.Root); err != nil {
					intact = false
				}
			}
		}
	}()
	for i := 0; i < 2; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			result := filepath.Join(t.TempDir(), "result.json")
			command := exec.Command(os.Args[0], "-test.run=^TestTargetCacheRepairWorker$")
			command.Env = append(os.Environ(), "NEUX_TEST_REPAIR_SOURCE="+root, "NEUX_TEST_REPAIR_CACHE="+cache, "NEUX_TEST_REPAIR_RESULT="+result)
			_, failures[index] = command.CombinedOutput()
			if failures[index] == nil {
				body, err := os.ReadFile(result)
				if err != nil {
					failures[index] = err
					return
				}
				var repaired NativeTarget
				failures[index] = json.Unmarshal(body, &repaired)
				results[index] = repaired.Root
			}
		}(i)
	}
	wait.Wait()
	close(stop)
	if !<-readDone {
		t.Fatal("repair created a gap in an existing root")
	}
	for _, err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if results[0] == "" || results[0] != results[1] {
		t.Fatal("two processes published competing repair roots", results)
	}
	if results[0] == target.Root {
		t.Fatal("repair overwrote previously returned immutable root")
	}
	if err := VerifyContent(results[0], target.Digest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target.Root, "damage.txt")); err != nil {
		t.Fatal("old root no longer recoverable", err)
	}
}

func TestTargetCacheRepairWorker(t *testing.T) {
	source := os.Getenv("NEUX_TEST_REPAIR_SOURCE")
	if source == "" {
		t.Skip("real subprocess helper")
	}
	pkg, err := Load(source)
	if err != nil {
		t.Fatal(err)
	}
	target, err := PrepareNativeTarget(t.Context(), os.Getenv("NEUX_TEST_REPAIR_CACHE"), "p", pkg, source, "claude")
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("NEUX_TEST_REPAIR_RESULT"), body, 0600); err != nil {
		t.Fatal(err)
	}
}
