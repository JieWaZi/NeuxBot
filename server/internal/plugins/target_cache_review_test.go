package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

func TestPrepareRuntimeTargetOwnsOnlySelectedTargetUntilRelease(t *testing.T) {
	root, parent := t.TempDir(), t.TempDir()
	standardInput(t, root, map[string]any{"name": "runtime-owner"})
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := ContentDigest(root)
	if err != nil {
		t.Fatal(err)
	}

	first, releaseFirst, err := PrepareRuntimeTarget(t.Context(), parent, "p", pkg, root, "claude", digest)
	if err != nil || releaseFirst == nil {
		t.Fatalf("first runtime preparation = %+v, error = %v", first, err)
	}
	t.Cleanup(releaseFirst)
	second, releaseSecond, err := PrepareRuntimeTarget(t.Context(), parent, "p", pkg, root, "claude", digest)
	if err != nil || releaseSecond == nil {
		t.Fatalf("second runtime preparation = %+v, error = %v", second, err)
	}
	t.Cleanup(releaseSecond)
	if first.Root == second.Root || first.Digest != second.Digest || first.SourceDigest != digest {
		t.Fatalf("independent leases did not produce the same bytes in distinct roots: %+v, %+v", first, second)
	}
	canonicalParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []NativeTarget{first, second} {
		if target.Runtime != "claude" || target.ManifestPath != ".claude-plugin/plugin.json" || filepath.Dir(target.Root) != canonicalParent {
			t.Fatalf("unexpected runtime target: %+v", target)
		}
		if err := VerifyContent(target.Root, target.Digest); err != nil {
			t.Fatal(err)
		}
	}
	if entries, err := os.ReadDir(parent); err != nil || len(entries) != 2 {
		t.Fatalf("runtime preparation created shared cache or extra targets: %v, %v", entries, err)
	}
	input, err := json.Marshal([]any{canonicalParent, "p", digest, "claude", pkg, []AuthenticationBinding(nil)})
	if err != nil {
		t.Fatal(err)
	}
	targetPreparationsMu.Lock()
	_, cached := targetPreparations[sha256.Sum256(input)]
	targetPreparationsMu.Unlock()
	if cached {
		t.Fatal("runtime preparation created a global cache entry")
	}

	releaseFirst()
	releaseFirst()
	if _, err := os.Stat(first.Root); !os.IsNotExist(err) {
		t.Fatalf("first lease did not dispose its own root: %v", err)
	}
	if err := VerifyContent(second.Root, second.Digest); err != nil {
		t.Fatalf("first lease removed an independent target: %v", err)
	}
	releaseSecond()
	if entries, err := os.ReadDir(parent); err != nil || len(entries) != 0 {
		t.Fatalf("runtime lease leaked a derived root: %v, %v", entries, err)
	}
}

func TestPrepareRuntimeTargetFailureDisposesStaging(t *testing.T) {
	root, parent := t.TempDir(), t.TempDir()
	standardInput(t, root, map[string]any{"name": "runtime-failure"})
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := ContentDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	fixtureFile(t, root, "changed.txt", "alternate content")
	if _, release, err := PrepareRuntimeTarget(t.Context(), parent, "p", pkg, root, "claude", digest); err == nil || release != nil {
		t.Fatalf("expected a rejected snapshot with its staging already disposed: %v", err)
	}
	if entries, err := os.ReadDir(parent); err != nil || len(entries) != 0 {
		t.Fatalf("failed runtime preparation leaked a derived root: %v, %v", entries, err)
	}
}

func TestPrepareRuntimeTargetDirectCodexReleaseKeepsPublishedSource(t *testing.T) {
	root, parent := t.TempDir(), filepath.Join(t.TempDir(), "targets")
	standardInput(t, root, map[string]any{"name": "direct-codex"})
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := ContentDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	target, release, err := PrepareRuntimeTarget(t.Context(), parent, "p", pkg, root, "codex", digest)
	if err != nil || release == nil {
		t.Fatalf("direct runtime preparation = %+v, error = %v", target, err)
	}
	release()
	if err := VerifyContent(root, digest); err != nil {
		t.Fatalf("direct target release mutated the published source: %v", err)
	}
	if _, err := os.Stat(parent); !os.IsNotExist(err) {
		t.Fatalf("direct target unnecessarily created a preparation directory: %v", err)
	}
}

func TestPrepareRuntimeTargetRejectsLinkedScratchRoot(t *testing.T) {
	root, temporary, outside := t.TempDir(), t.TempDir(), t.TempDir()
	standardInput(t, root, map[string]any{"name": "linked-runtime-root"})
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := ContentDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(temporary, "targets")
	if err := os.Symlink(outside, parent); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("directory symlinks unavailable: %v", err)
		}
		t.Fatal(err)
	}
	target, release, err := PrepareRuntimeTarget(t.Context(), parent, "p", pkg, root, "claude", digest)
	if release != nil {
		release()
	}
	if err == nil {
		t.Fatalf("runtime preparation followed a linked scratch root: %s", target.Root)
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("runtime preparation used a directory outside its managed root: %v, %v", entries, err)
	}
}

func TestTargetPreparationBoundsIdleEntriesWithoutEvictingActiveKey(t *testing.T) {
	root, parent := t.TempDir(), t.TempDir()
	standardInput(t, root, map[string]any{"name": "bounded-cache"})
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := ContentDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	entry, release, err := lockTargetPreparation(t.Context(), parent, "held", digest, "claude", pkg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for i := 0; i < 200; i++ {
		_, releaseOther, err := lockTargetPreparation(t.Context(), parent, strconv.Itoa(i), digest, "claude", pkg, nil)
		if err != nil {
			t.Fatal(err)
		}
		releaseOther()
	}
	targetPreparationsMu.Lock()
	count := len(targetPreparations)
	targetPreparationsMu.Unlock()
	if count > 129 {
		t.Fatalf("idle preparations were retained beyond the cache bound: %d", count)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	if _, unlock, err := lockTargetPreparation(ctx, parent, "held", digest, "claude", pkg, nil); !errors.Is(err, context.DeadlineExceeded) {
		if unlock != nil {
			unlock()
		}
		t.Fatalf("cache pressure evicted an active preparation: %v", err)
	}
	release()
	again, unlock, err := lockTargetPreparation(t.Context(), parent, "held", digest, "claude", pkg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if again != entry {
		t.Fatal("recently released preparation was not reused")
	}
}

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
