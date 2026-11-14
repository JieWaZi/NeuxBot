package plugins

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestStageBindsSnapshotAndRejectsChangedContent(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, "plugin.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"snapshot"}`)
	fixtureFile(t, root, "README.md", "original")
	parent := t.TempDir()
	staged, err := Stage(parent, root)
	if err != nil {
		t.Fatal(err)
	}
	fixtureFile(t, root, "README.md", "source changed")
	if err := VerifyContent(staged.Root, staged.Digest); err != nil {
		t.Fatalf("source mutation changed snapshot: %v", err)
	}
	fixtureFile(t, staged.Root, "README.md", "managed snapshot changed")
	if err := VerifyContent(staged.Root, staged.Digest); err == nil {
		t.Fatal("accepted changed managed snapshot")
	}
}

func TestStageRejectsZIPTraversalCaseCollisionAndEscapingLinks(t *testing.T) {
	for _, entries := range []map[string]string{{"../escaped": "bad"}, {"A.txt": "first", "a.txt": "second"}} {
		path := filepath.Join(t.TempDir(), "package.zip")
		output, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		archive := zip.NewWriter(output)
		for name, content := range entries {
			file, e := archive.Create(name)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = file.Write([]byte(content)); e != nil {
				t.Fatal(e)
			}
		}
		if err := archive.Close(); err != nil {
			t.Fatal(err)
		}
		if err := output.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := Stage(t.TempDir(), path); err == nil {
			t.Fatalf("accepted unsafe ZIP: %v", entries)
		}
	}
	root := t.TempDir()
	fixtureFile(t, root, "plugin.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"links"}`)
	outside := t.TempDir()
	fixtureFile(t, outside, "private", "private")
	if err := os.Symlink(outside, filepath.Join(root, "escaped")); err != nil {
		t.Fatal(err)
	}
	if _, err := Stage(t.TempDir(), root); err == nil {
		t.Fatal("accepted escaping package symlink")
	}
}

func TestContentDigestFramesFileBoundariesAndRejectsCollidingReplacement(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	for _, root := range []string{a, b} {
		fixtureFile(t, root, "plugin.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"frames"}`)
	}
	fixtureFile(t, a, "a", "xb\x00\x00y")
	fixtureFile(t, b, "a", "x")
	fixtureFile(t, b, "b", "y")
	parent := t.TempDir()
	first, err := Stage(parent, a)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Stage(parent, b)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest == second.Digest {
		t.Error("different file layouts share a content identity")
	}
	data := t.TempDir()
	one, err := CopyContent(data, first.Root, first.Digest)
	if err != nil {
		t.Fatal(err)
	}
	two, err := CopyContent(data, second.Root, second.Digest)
	if err != nil {
		t.Fatal(err)
	}
	if one == two {
		t.Error("second package reused first content")
	}
	if _, err := os.Stat(filepath.Join(two, "b")); err != nil {
		t.Error("second package lost b", err)
	}
	// 重新分段的文件树不能复用暂存内容的原摘要。
	fixtureFile(t, first.Root, "a", "x")
	fixtureFile(t, first.Root, "b", "y")
	if err := VerifyContent(first.Root, first.Digest); err == nil {
		t.Error("accepted a framed-content staging replacement")
	}
}

func TestStageSourceNeverRebasesAnExplicitEntry(t *testing.T) {
	root := t.TempDir()
	standardInput(t, filepath.Join(root, "wrapper", "entry"), nil)
	if _, err := StageSource(t.TempDir(), root, "entry"); err == nil {
		t.Fatal("fixed published entry was silently moved into a wrapper")
	}
	if _, err := StageSource(t.TempDir(), root, "wrapper/entry"); err != nil {
		t.Fatal(err)
	}
}
