package main

import (
	"archive/zip"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/packagefiles"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCommandPackagesAllTargetsWithPortableProvenance(t *testing.T) {
	source, output := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "plugin.json"), []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"release-test","version":"1.2.3"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "skills/review"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "skills/review/SKILL.md"), []byte("---\nname: review\ndescription: Review\n---\nReview"), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".", "--source", source, "--output", output, "--profile", "all", "--source-id", "test/release")
	if data, err := command.CombinedOutput(); err != nil {
		t.Fatalf("packager unavailable: %v\n%s", err, data)
	}
	body, err := os.ReadFile(filepath.Join(output, "artifacts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), source) || strings.Contains(string(body), output) {
		t.Fatal("machine paths leaked into release manifest")
	}
	var manifest struct {
		// SchemaVersion 是发布工件契约版本。
		SchemaVersion int `json:"schemaVersion"`
		// Artifacts 是按目标发布的工件。
		Artifacts []struct {
			// Archive 是发布根内归档位置。
			Archive string `json:"archive"`
			// ContentDigest 是完整目标摘要。
			ContentDigest string `json:"contentDigest"`
			// ArchiveSHA256 是 ZIP 字节摘要。
			ArchiveSHA256 string `json:"archiveSHA256"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(body, &manifest); err != nil || manifest.SchemaVersion != 1 || len(manifest.Artifacts) != 10 {
		t.Fatalf("incomplete manifest: %s %v", body, err)
	}
	for _, artifact := range manifest.Artifacts {
		if len(artifact.ContentDigest) != 64 || len(artifact.ArchiveSHA256) != 64 {
			t.Fatal("missing independent digests")
		}
		archive, err := zip.OpenReader(filepath.Join(output, artifact.Archive))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, file := range archive.File {
			if file.Name == "skills/review/SKILL.md" {
				found = true
			}
			if strings.Contains(file.Name, "../") {
				t.Fatal("archive references outside root")
			}
		}
		archive.Close()
		if !found {
			t.Fatal("target archive lost skill resources")
		}
	}
}

func TestCommandRejectsUnknownProfileAndDigestMismatch(t *testing.T) {
	for _, args := range [][]string{{"--profile", "unknown"}, {"--source-digest", strings.Repeat("0", 64)}} {
		source, output := t.TempDir(), t.TempDir()
		if err := os.WriteFile(filepath.Join(source, "plugin.json"), []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"invalid"}`), 0600); err != nil {
			t.Fatal(err)
		}
		parameters := append([]string{"run", ".", "--source", source, "--output", output}, args...)
		if data, err := exec.Command("go", parameters...).CombinedOutput(); err == nil {
			t.Fatalf("invalid inputs accepted: %s", data)
		}
		if _, err := os.Stat(filepath.Join(output, "artifacts.json")); !os.IsNotExist(err) {
			t.Fatal("invalid release published success manifest")
		}
	}
}

func TestReleaseRejectsPrivateHostFilesAndLocalSourceIdentities(t *testing.T) {
	for _, name := range []string{".git", ".env", "nested/.env.local"} {
		root := releaseFixture(t)
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fictional local material"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := run([]string{"--source", root, "--output", filepath.Join(t.TempDir(), "release"), "--profile", "codex"}); err == nil {
			t.Fatal("private host material entered release", name)
		}
	}
	for _, identity := range []string{"file:///fictional/private/source", "C:/fictional/private", "https://user@example.test/repo", "../private/source"} {
		if err := run([]string{"--source", releaseFixture(t), "--output", filepath.Join(t.TempDir(), "release"), "--source-id", identity, "--profile", "codex"}); err == nil {
			t.Fatal("local/private source identity published", identity)
		}
	}
}

func TestReleaseRejectsOutputAliasInsideSourceBeforeCreatingStage(t *testing.T) {
	root := releaseFixture(t)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skip(err)
	}
	before, err := plugins.ContentDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"--source", root, "--output", filepath.Join(alias, "out"), "--profile", "codex"}); err == nil {
		t.Fatal("output alias recursed into source")
	}
	after, err := plugins.ContentDigest(root)
	if err != nil || after != before {
		t.Fatal("rejected output mutated source", err)
	}
}

func TestZIPRestoresEmptyDirectoriesAndSafeLinksWithExactDigest(t *testing.T) {
	source := releaseFixture(t)
	if err := os.MkdirAll(filepath.Join(source, "skills/review/empty"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("SKILL.md", filepath.Join(source, "skills/review/guide.md")); err != nil {
		t.Skip(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".env.example"), []byte("PUBLIC_EXAMPLE=value"), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "release")
	if err := run([]string{"--source", source, "--output", output, "--profile", "codex"}); err != nil {
		t.Fatalf("safe release cannot package internal links: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(output, "artifacts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest releaseManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Artifacts) != 1 {
		t.Fatal("single profile generated other targets")
	}
	item := manifest.Artifacts[0]
	unpacked := t.TempDir()
	archive := packagefiles.Archive{Invalid: status_error.PluginContentInvalid, IO: status_error.PluginStorageError, Limit: status_error.PluginContentLimitExceeded, PreserveExecutable: true}
	if err := archive.ExtractZIP(filepath.Join(output, item.Archive), unpacked); err != nil {
		t.Fatal(err)
	}
	actual, err := plugins.ContentDigest(unpacked)
	if err != nil || actual != item.ContentDigest {
		t.Fatal("ZIP and declared content digest differ", actual, item.ContentDigest, err)
	}
	if link, err := os.Readlink(filepath.Join(unpacked, "skills/review/guide.md")); err != nil || link != "SKILL.md" {
		t.Fatal("safe symlink lost", err)
	}
	if info, err := os.Stat(filepath.Join(unpacked, "skills/review/empty")); err != nil || !info.IsDir() {
		t.Fatal("empty resource directory lost", err)
	}
}

func releaseFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"review-release","version":"1.2.3"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "skills/review"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills/review/SKILL.md"), []byte("---\nname: review\ndescription: Review\n---\nReview"), 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestReleaseKeepsOneFrozenSourceWhileOriginalChanges(t *testing.T) {
	source := releaseFixture(t)
	revision := filepath.Join(source, "revision.txt")
	if err := os.WriteFile(revision, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "asset.bin"), []byte(strings.Repeat("a", 4*1024*1024)), 0600); err != nil {
		t.Fatal(err)
	}
	expected, err := plugins.ContentDigest(source)
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	output := filepath.Join(parent, "release")
	changed := make(chan error, 1)
	stop := make(chan struct{})
	go func() {
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		for {
			select {
			case <-stop:
				return
			case <-deadline.C:
				changed <- status_error.PluginNotReady
				return
			default:
				paths, _ := filepath.Glob(filepath.Join(parent, ".pluginpack-*", ".targets", "native-*"))
				if len(paths) > 0 {
					changed <- os.WriteFile(revision, []byte("after"), 0600)
					return
				}
				time.Sleep(time.Millisecond)
			}
		}
	}()
	err = run([]string{"--source", source, "--output", output, "--profile", "all", "--source-digest", expected})
	close(stop)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-changed:
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("test did not mutate the original during target generation")
	}
	body, err := os.ReadFile(filepath.Join(output, "artifacts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest releaseManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SourceDigest != expected {
		t.Fatal("receipt changed accepted source digest")
	}
	for _, artifact := range manifest.Artifacts {
		body, err := os.ReadFile(filepath.Join(output, artifact.Directory, "revision.txt"))
		if err != nil || string(body) != "before" {
			t.Fatal("target used mutable original instead of frozen input", artifact.Runtime, err, string(body))
		}
	}
}

func TestReleaseNormalizesAbsoluteAndPreservesRelativeLinks(t *testing.T) {
	for _, absolute := range []bool{true, false} {
		source := releaseFixture(t)
		target := filepath.Join(source, "skills/review/SKILL.md")
		link := target
		if !absolute {
			link = "skills/review/SKILL.md"
		}
		if err := os.Symlink(link, filepath.Join(source, "guide.md")); err != nil {
			t.Skip(err)
		}
		original, err := plugins.ContentDigest(source)
		if err != nil {
			t.Fatal(err)
		}
		output := filepath.Join(t.TempDir(), "release")
		if err := run([]string{"--source", source, "--output", output, "--profile", "codex", "--source-digest", original}); err != nil {
			t.Fatal("same legal input accepted by host but rejected by packager", err)
		}
		body, err := os.ReadFile(filepath.Join(output, "artifacts.json"))
		if err != nil {
			t.Fatal(err)
		}
		var manifest map[string]any
		if json.Unmarshal(body, &manifest) != nil {
			t.Fatal("invalid receipt")
		}
		normalized, ok := manifest["normalizedSourceDigest"].(string)
		if !ok || len(normalized) != 64 || absolute && normalized == original || !absolute && normalized != original || manifest["sourceDigest"] != original {
			t.Fatal("receipt lost original/normalized source binding", manifest)
		}
		after, err := plugins.ContentDigest(source)
		if err != nil || after != original {
			t.Fatal("publication modified original", err)
		}
		alias := filepath.Join(output, "review-release-codex/guide.md")
		text, err := os.Readlink(alias)
		if err != nil || filepath.IsAbs(text) || strings.Contains(text, filepath.Base(source)) {
			t.Fatal("published link still depends on source location", text, err)
		}
	}
}

func TestFreezeSourceBindsCopiedRawBytesToAcceptedDigestDuringABAChange(t *testing.T) {
	source := releaseFixture(t)
	revision := filepath.Join(source, "revision.txt")
	if err := os.WriteFile(revision, []byte("version-A"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "000-asset.bin"), []byte(strings.Repeat("a", 16*1024*1024)), 0600); err != nil {
		t.Fatal(err)
	}
	expected, err := plugins.ContentDigest(source)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "frozen")
	result := make(chan error, 1)
	stop := make(chan struct{})
	go func() {
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		changed := false
		for {
			select {
			case <-stop:
				return
			case <-deadline.C:
				result <- status_error.PluginNotReady
				return
			default:
				if !changed {
					if _, err := os.Stat(destination); err == nil {
						if err := os.WriteFile(revision, []byte("version-B"), 0600); err != nil {
							result <- err
							return
						}
						changed = true
					}
				}
				if changed {
					if body, err := os.ReadFile(filepath.Join(destination, "revision.txt")); err == nil && string(body) == "version-B" {
						result <- os.WriteFile(revision, []byte("version-A"), 0600)
						return
					}
				}
				runtime.Gosched()
			}
		}
	}()
	frozen, freezeErr := freezeReleaseSource(source, destination, expected)
	close(stop)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("fixture did not perform A-to-B-to-A during raw snapshot copy")
	}
	if freezeErr == nil {
		body, err := os.ReadFile(filepath.Join(frozen.Root, "revision.txt"))
		if err != nil || string(body) != "version-A" {
			t.Fatal("raw copied version B was accepted using digest for version A", err)
		}
	}
}
