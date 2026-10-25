package skills

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	gitclient "github.com/go-git/go-git/v5/plumbing/transport/client"
	gitserver "github.com/go-git/go-git/v5/plumbing/transport/server"
)

func TestNormalizeGitSourceSupportsGitHubTreeURL(t *testing.T) {
	source, err := normalizeGitSource(Source{
		Kind:     "git",
		Location: "https://github.com/example/skills/tree/main/catalog",
		Subpath:  "review",
	})
	if err != nil {
		t.Fatalf("normalizeGitSource() error = %v", err)
	}
	if source.Location != "https://github.com/example/skills.git" {
		t.Fatalf("Location = %q", source.Location)
	}
	if source.Ref != "main" {
		t.Fatalf("Ref = %q", source.Ref)
	}
	if source.Subpath != "catalog/review" {
		t.Fatalf("Subpath = %q", source.Subpath)
	}
}

func TestNormalizeGitSourceRejectsCredentials(t *testing.T) {
	_, err := normalizeGitSource(Source{
		Kind:     "git",
		Location: "https://token@example.com/skills.git",
	})
	if err == nil {
		t.Fatal("normalizeGitSource() error = nil, want credentials rejected")
	}
}

func TestCloneGitSourceChecksOutCommitRef(t *testing.T) {
	repository := t.TempDir()
	gitRepository, err := git.PlainInit(repository, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "SKILL.md"), []byte("---\nname: review\ndescription: Review changes\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	worktree, err := gitRepository.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add("SKILL.md"); err != nil {
		t.Fatal(err)
	}
	commit, err := worktree.Commit("initial", &git.CommitOptions{Author: &object.Signature{
		Name: "NeuxBot Test", Email: "ally@example.test", When: time.Unix(1, 0),
	}})
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := transport.NewEndpoint(repository)
	if err != nil {
		t.Fatal(err)
	}
	previousFileTransport := gitclient.Protocols["file"]
	gitclient.Protocols["file"] = gitserver.NewClient(gitserver.MapLoader{
		endpoint.String(): gitRepository.Storer,
	})
	defer func() { gitclient.Protocols["file"] = previousFileTransport }()
	t.Setenv("PATH", "")
	destination := filepath.Join(t.TempDir(), "checkout")
	if err := cloneGitSource(context.Background(), Source{
		Location: repository,
		Ref:      commit.String(),
	}, destination); err != nil {
		t.Fatalf("cloneGitSource() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "SKILL.md")); err != nil {
		t.Fatalf("checked out SKILL.md: %v", err)
	}
}

func TestCloneGitSourceFallsBackFromBranchToTag(t *testing.T) {
	repository := t.TempDir()
	gitRepository, err := git.PlainInit(repository, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(repository, "SKILL.md"),
		[]byte("---\nname: tagged\ndescription: Tagged skill\n---\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	worktree, err := gitRepository.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add("SKILL.md"); err != nil {
		t.Fatal(err)
	}
	commit, err := worktree.Commit("tagged", &git.CommitOptions{Author: &object.Signature{
		Name: "NeuxBot Test", Email: "ally@example.test", When: time.Unix(2, 0),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gitRepository.CreateTag("v1.0.0", commit, nil); err != nil {
		t.Fatal(err)
	}
	endpoint, err := transport.NewEndpoint(repository)
	if err != nil {
		t.Fatal(err)
	}
	previousFileTransport := gitclient.Protocols["file"]
	gitclient.Protocols["file"] = gitserver.NewClient(gitserver.MapLoader{
		endpoint.String(): gitRepository.Storer,
	})
	defer func() { gitclient.Protocols["file"] = previousFileTransport }()
	previousClone := plainCloneContext
	cloneAttempts := 0
	plainCloneContext = func(
		ctx context.Context,
		destination string,
		bare bool,
		options *git.CloneOptions,
	) (*git.Repository, error) {
		cloneAttempts++
		if cloneAttempts == 1 {
			if err := os.MkdirAll(destination, 0o700); err != nil {
				return nil, err
			}
			if err := os.WriteFile(filepath.Join(destination, ".partial-clone"), []byte("partial"), 0o600); err != nil {
				return nil, err
			}
			return nil, plumbing.ErrReferenceNotFound
		}
		return previousClone(ctx, destination, bare, options)
	}
	defer func() { plainCloneContext = previousClone }()
	destination := filepath.Join(t.TempDir(), "checkout")
	if err := cloneGitSource(context.Background(), Source{
		Location: repository,
		Ref:      "v1.0.0",
	}, destination); err != nil {
		t.Fatalf("cloneGitSource() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "SKILL.md")); err != nil {
		t.Fatalf("tagged SKILL.md: %v", err)
	}
	if cloneAttempts != 2 {
		t.Fatalf("clone attempts = %d, want branch then tag", cloneAttempts)
	}
}

func TestStageImportAllowsSymlinkInCompleteGitCheckout(t *testing.T) {
	repository := t.TempDir()
	gitRepository, err := git.PlainInit(repository, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(repository, "SKILL.md"),
		[]byte("---\nname: git-tree\ndescription: Git tree\n---\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repository, "references", "deep", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(
		filepath.Join(repository, "SKILL.md"),
		filepath.Join(repository, "references", "deep", "nested", "linked.md"),
	); err != nil {
		t.Skipf("filesystem does not support symbolic links: %v", err)
	}
	worktree, err := gitRepository.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add("SKILL.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add("references/deep/nested/linked.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Commit("unsafe tree", &git.CommitOptions{Author: &object.Signature{
		Name: "NeuxBot Test", Email: "ally@example.test", When: time.Unix(3, 0),
	}}); err != nil {
		t.Fatal(err)
	}
	location := "https://example.test/skills.git"
	endpoint, err := transport.NewEndpoint(location)
	if err != nil {
		t.Fatal(err)
	}
	previousHTTPSTransport := gitclient.Protocols["https"]
	gitclient.Protocols["https"] = gitserver.NewClient(gitserver.MapLoader{
		endpoint.String(): gitRepository.Storer,
	})
	defer func() { gitclient.Protocols["https"] = previousHTTPSTransport }()
	previousClone := plainCloneContext
	plainCloneContext = func(
		ctx context.Context,
		destination string,
		bare bool,
		options *git.CloneOptions,
	) (*git.Repository, error) {
		clonedOptions := *options
		clonedOptions.Depth = 0
		return previousClone(ctx, destination, bare, &clonedOptions)
	}
	defer func() { plainCloneContext = previousClone }()

	batch, err := StageImport(
		context.Background(),
		t.TempDir(),
		Source{Kind: "git", Location: location},
	)
	if err != nil {
		t.Fatalf("StageImport() error = %v, want symlink accepted", err)
	}
	defer os.RemoveAll(batch.Root)
	linkedPath := filepath.Join(batch.Root, stagedBatchContentName, "references", "deep", "nested", "linked.md")
	linkedInfo, err := os.Lstat(linkedPath)
	if err != nil {
		t.Fatal(err)
	}
	if linkedInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("linked entry mode = %v, want symlink", linkedInfo.Mode())
	}
}

func TestImportZipScansRootAndTwoNestedLevels(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "skills.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, name := range []string{"one/SKILL.md", "skills/two/SKILL.md", "skills/two/references/ignored.md", "skills/three/deep/SKILL.md"} {
		entry, createErr := writer.Create(name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := entry.Write([]byte("---\nname: test\ndescription: Test skill\n---\n")); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	packages, root, err := Import(context.Background(), t.TempDir(), Source{Kind: "zip", Location: archivePath})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if len(packages) != 2 {
		t.Fatalf("packages = %d, want 2", len(packages))
	}
}

func TestOpenStagedImportRejectsContentChangedAfterScan(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "tamper.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, content := range map[string]string{
		"SKILL.md":            "---\nname: tamper\ndescription: Tamper test\n---\n",
		"references/guide.md": "before",
	} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	temporaryParent := t.TempDir()
	batch, err := StageImport(
		context.Background(),
		temporaryParent,
		Source{Kind: "zip", Location: archivePath},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(batch.Root, stagedBatchContentName, "references", "guide.md"),
		[]byte("after"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := OpenStagedImport(temporaryParent, batch.Token); err == nil {
		t.Fatal("OpenStagedImport() error = nil, want changed staging rejected")
	}
}

func TestImportZipKeepsScanningBelowRootCandidate(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "skills-with-root.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entries := map[string]string{
		"SKILL.md":             "root-skill",
		"nested/SKILL.md":      "nested-skill",
		"nested/deep/SKILL.md": "deep-skill",
	}
	for name, skillName := range entries {
		entry, createErr := writer.Create(name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		content := "---\nname: " + skillName + "\ndescription: Test skill\n---\n"
		if _, writeErr := entry.Write([]byte(content)); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	packages, root, err := Import(
		context.Background(),
		t.TempDir(),
		Source{Kind: "zip", Location: archivePath},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if len(packages) != 3 {
		t.Fatalf("packages = %d, want 3", len(packages))
	}
}

func TestImportRejectsZipPathTraversal(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "unsafe.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("../SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("x"))
	_ = writer.Close()
	_ = file.Close()
	if _, _, err := Import(context.Background(), t.TempDir(), Source{Kind: "zip", Location: archivePath}); err == nil {
		t.Fatal("expected traversal rejection")
	}
}

func TestImportRejectsZipSourceOverBudget(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "oversized.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(100*1024*1024+1, 0); err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("---\nname: oversized\ndescription: Oversized skill\n---\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if _, _, err := Import(
		context.Background(),
		t.TempDir(),
		Source{Kind: "zip", Location: archivePath},
	); err == nil {
		t.Fatal("Import() error = nil, want ZIP source budget rejection")
	}
}

func TestImportRejectsBatchWithMoreThanFiveThousandFiles(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "too-many-files.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("---\nname: many-files\ndescription: Many files\n---\n")); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 5000; index++ {
		entry, err := writer.Create(fmt.Sprintf("references/%04d.txt", index))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if _, _, err := Import(
		context.Background(),
		t.TempDir(),
		Source{Kind: "zip", Location: archivePath},
	); err == nil {
		t.Fatal("Import() error = nil, want file count rejection")
	}
}

func TestImportRejectsSingleFileOverFiftyMiB(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "large-file.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("assets/large.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.CopyN(entry, zeroReader{}, 50*1024*1024+1); err != nil {
		t.Fatal(err)
	}
	entry, err = writer.Create("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("---\nname: large-file\ndescription: Large file\n---\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if _, _, err := Import(
		context.Background(),
		t.TempDir(),
		Source{Kind: "zip", Location: archivePath},
	); err == nil {
		t.Fatal("Import() error = nil, want single file budget rejection")
	}
}

func TestImportRejectsRelativePathOverFiveHundredTwelveBytes(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "long-path.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, content := range map[string]string{
		"SKILL.md": "---\nname: long-path\ndescription: Long path\n---\n",
		strings.Repeat("a", 170) + "/" + strings.Repeat("b", 170) + "/" + strings.Repeat("c", 170) + ".txt": "x",
	} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if _, _, err := Import(
		context.Background(),
		t.TempDir(),
		Source{Kind: "zip", Location: archivePath},
	); err == nil {
		t.Fatal("Import() error = nil, want path length rejection")
	}
}

func TestImportRejectsCaseFoldedPathConflict(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "case-conflict.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, content := range map[string]string{
		"SKILL.md":        "---\nname: case-conflict\ndescription: Case conflict\n---\n",
		"Assets/File.txt": "one",
		"assets/file.txt": "two",
	} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if _, _, err := Import(
		context.Background(),
		t.TempDir(),
		Source{Kind: "zip", Location: archivePath},
	); err == nil {
		t.Fatal("Import() error = nil, want case-folded path conflict")
	}
}

func TestImportRejectsWindowsIllegalAndNULPaths(t *testing.T) {
	for _, invalidPath := range []string{
		"references/CON.txt",
		"references/trailing. ",
		"references/bad?.txt",
		"references/nul\x00.txt",
	} {
		t.Run(fmt.Sprintf("%q", invalidPath), func(t *testing.T) {
			archivePath := filepath.Join(t.TempDir(), "invalid-path.zip")
			file, err := os.Create(archivePath)
			if err != nil {
				t.Fatal(err)
			}
			writer := zip.NewWriter(file)
			for name, content := range map[string]string{
				"SKILL.md":  "---\nname: invalid-path\ndescription: Invalid path\n---\n",
				invalidPath: "invalid",
			} {
				entry, err := writer.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := entry.Write([]byte(content)); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}

			if _, _, err := Import(
				context.Background(),
				t.TempDir(),
				Source{Kind: "zip", Location: archivePath},
			); err == nil {
				t.Fatalf("Import() error = nil, want path %q rejected", invalidPath)
			}
		})
	}
}

func TestImportAllowsZipSymbolicLink(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "symbolic-link.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	skill, err := writer.Create("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := skill.Write([]byte("---\nname: linked\ndescription: Linked skill\n---\n")); err != nil {
		t.Fatal(err)
	}
	header := &zip.FileHeader{Name: "references/link.txt", Method: zip.Store}
	header.SetMode(os.ModeSymlink | 0o777)
	link, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := link.Write([]byte("target.txt")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	packages, root, err := Import(
		context.Background(),
		t.TempDir(),
		Source{Kind: "zip", Location: archivePath},
	)
	if err != nil {
		t.Fatalf("Import() error = %v, want symbolic link accepted", err)
	}
	defer os.RemoveAll(root)
	linkInfo, err := os.Lstat(filepath.Join(packages[0].Directory, "references", "link.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link mode = %v, want symlink", linkInfo.Mode())
	}
}

func TestImportRejectsZipEntryBelowSymlink(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "symlink-parent.zip")
	outside := t.TempDir()
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	skill, err := writer.Create("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := skill.Write([]byte("---\nname: linked-parent\ndescription: Linked parent\n---\n")); err != nil {
		t.Fatal(err)
	}
	header := &zip.FileHeader{Name: "references", Method: zip.Store}
	header.SetMode(os.ModeSymlink | 0o777)
	link, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := link.Write([]byte(outside)); err != nil {
		t.Fatal(err)
	}
	nested, err := writer.Create("references/escaped.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nested.Write([]byte("escaped")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if _, _, err := Import(
		context.Background(),
		t.TempDir(),
		Source{Kind: "zip", Location: archivePath},
	); err == nil {
		t.Fatal("Import() error = nil, want entry below symlink rejected")
	}
	if _, err := os.Stat(filepath.Join(outside, "escaped.txt")); !os.IsNotExist(err) {
		t.Fatalf("escaped file stat error = %v, want file absent", err)
	}
}

func TestImportPreservesExecutableModeForScripts(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "executable-script.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	skill, err := writer.Create("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := skill.Write([]byte("---\nname: executable\ndescription: Executable skill\n---\n")); err != nil {
		t.Fatal(err)
	}
	header := &zip.FileHeader{Name: "scripts/run.sh", Method: zip.Store}
	header.SetMode(0o755)
	script, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := script.Write([]byte("#!/bin/sh\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	packages, root, err := Import(
		context.Background(),
		t.TempDir(),
		Source{Kind: "zip", Location: archivePath},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	info, err := os.Stat(filepath.Join(packages[0].Directory, "scripts", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("script mode = %o, want executable bit", info.Mode().Perm())
	}
}

func TestValidateStagedTreeAllowsSymlinkOutsideCandidateDepth(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "catalog", "review", "references", "deep"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "catalog", "review", "SKILL.md"),
		[]byte("---\nname: review\ndescription: Review skill\n---\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(
		filepath.Join(root, "catalog", "review", "SKILL.md"),
		filepath.Join(root, "catalog", "review", "references", "deep", "linked.md"),
	); err != nil {
		t.Skipf("filesystem does not support symbolic links: %v", err)
	}

	if _, err := validateStagedTree(root); err != nil {
		t.Fatalf("validateStagedTree() error = %v, want symlink accepted", err)
	}
}

func TestValidateStagedTreeRejectsCumulativeContentOverFiveHundredMiB(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(root, "SKILL.md"),
		[]byte("---\nname: cumulative\ndescription: Cumulative budget\n---\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 10; index++ {
		path := filepath.Join(root, "assets", fmt.Sprintf("%02d.bin", index))
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(50 * 1024 * 1024); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := validateStagedTree(root); err == nil {
		t.Fatal("validateStagedTree() error = nil, want cumulative budget rejection")
	}
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	clear(buffer)
	return len(buffer), nil
}

func TestCopyVersionUsesSemanticVersionAndLeavesNoStagingDirectory(t *testing.T) {
	dataDirectory := t.TempDir()
	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(source, "SKILL.md"),
		[]byte("---\nname: review\ndescription: Review changes\n---\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	relative, digest, err := CopyVersion(dataDirectory, "skill-review", "1.2.3", source)
	if err != nil {
		t.Fatal(err)
	}
	if relative != filepath.Join("skills", "versions", "skill-review", "1.2.3") {
		t.Fatalf("relative = %q", relative)
	}
	if digest == "" {
		t.Fatal("digest is empty")
	}
	entries, err := os.ReadDir(filepath.Dir(filepath.Join(dataDirectory, relative)))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "1.2.3" {
		t.Fatalf("version directory entries = %v", entries)
	}
	if _, _, err := CopyVersion(dataDirectory, "skill-review", "1.2.3", source); err == nil {
		t.Fatal("duplicate semantic version directory was overwritten")
	}
}

func TestCopyVersionRejectsNonPortablePath(t *testing.T) {
	dataDirectory := t.TempDir()
	source := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(source, "SKILL.md"),
		[]byte("---\nname: review\ndescription: Review changes\n---\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "bad:name.txt"), []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, err := CopyVersion(dataDirectory, "skill-review", "1.0.0", source); err == nil {
		t.Fatal("CopyVersion() error = nil, want non-portable path rejected")
	}
	if _, err := os.Stat(filepath.Join(
		dataDirectory,
		"skills",
		"versions",
		"skill-review",
		"1.0.0",
	)); !os.IsNotExist(err) {
		t.Fatalf("invalid version directory exists: %v", err)
	}
}

func TestDigestIncludesScriptExecutableMode(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o700); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "scripts", "run.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := Digest(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(script, 0o700); err != nil {
		t.Fatal(err)
	}
	after, err := Digest(root)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatalf("Digest() = %q before and after executable mode change", before)
	}
}

func TestCopyVersionPreservesExecutableScripts(t *testing.T) {
	dataDirectory := t.TempDir()
	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(filepath.Join(source, "scripts"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(source, "SKILL.md"),
		[]byte("---\nname: executable-copy\ndescription: Executable copy\n---\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "scripts", "run.sh"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	relative, _, err := CopyVersion(dataDirectory, "skill-executable", "1.0.0", source)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dataDirectory, relative, "scripts", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("copied script mode = %o, want executable bit", info.Mode().Perm())
	}
}

func TestCopyVersionRejectsHardLinkedFiles(t *testing.T) {
	source := t.TempDir()
	skillPath := filepath.Join(source, "SKILL.md")
	if err := os.WriteFile(
		skillPath,
		[]byte("---\nname: hard-linked\ndescription: Hard linked\n---\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(skillPath, filepath.Join(source, "linked.md")); err != nil {
		t.Skipf("filesystem does not support hard links: %v", err)
	}

	if _, _, err := CopyVersion(t.TempDir(), "skill-hard-linked", "1.0.0", source); err == nil {
		t.Fatal("CopyVersion() error = nil, want hard link rejection")
	}
}

func TestCopyVersionExcludesGitMetadata(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(source, "SKILL.md"),
		[]byte("---\nname: git-root\ndescription: Git root\n---\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, ".git", "objects"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".git", "config"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	dataDirectory := t.TempDir()
	relative, _, err := CopyVersion(dataDirectory, "skill-git-root", "1.0.0", source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dataDirectory, relative, ".git")); err == nil {
		t.Fatal("copied version contains .git metadata")
	}
}
