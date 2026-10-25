package stdio

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLauncherPreservesWorkingDirectoryStreamsAndArgumentBoundaries(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	data := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	data, _ = filepath.EvalSymlinks(data)
	t.Setenv("PLUGIN_ROOT", root)
	t.Setenv("PLUGIN_DATA", data)
	t.Setenv("NEUXALLY_STDIO_FIXTURE", "1")
	var output bytes.Buffer
	code := Run(context.Background(), root, []string{binary, "-test.run=TestLauncherChild", "--", "space argument", "literal&value"}, bytes.NewBufferString("input payload"), &output, io.Discard)
	if code != 0 {
		t.Fatalf("child exit %d: %s", code, output.String())
	}
	var result map[string]string
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["cwd"] != root || result["arg"] != "literal&value" || result["input"] != "input payload" {
		t.Fatalf("process boundary changed: %+v", result)
	}
	if code := Run(context.Background(), filepath.Dir(root), []string{binary}, bytes.NewReader(nil), io.Discard, io.Discard); code == 0 {
		t.Fatal("launcher accepted working directory outside plugin roots")
	}
}
func TestLauncherCancellationTerminatesChild(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	t.Setenv("PLUGIN_ROOT", root)
	t.Setenv("PLUGIN_DATA", root)
	t.Setenv("NEUXALLY_STDIO_FIXTURE", "wait")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	if code := Run(ctx, root, []string{binary, "-test.run=TestLauncherChild"}, bytes.NewReader(nil), io.Discard, io.Discard); code == 0 {
		t.Fatal("cancelled child succeeded")
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("child remained alive after cancellation")
	}
}
func TestLauncherChild(t *testing.T) {
	mode := os.Getenv("NEUXALLY_STDIO_FIXTURE")
	if mode == "" {
		return
	}
	if mode == "wait" {
		time.Sleep(10 * time.Minute)
		os.Exit(0)
	}
	cwd, _ := os.Getwd()
	data, _ := io.ReadAll(os.Stdin)
	_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"cwd": cwd, "arg": os.Args[len(os.Args)-1], "input": string(data)})
	os.Exit(0)
}
