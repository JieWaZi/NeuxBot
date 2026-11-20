package cliinstall

import "testing"

func TestAppendWindowsPathPreservesExistingEntries(t *testing.T) {
	const original = `%USERPROFILE%\bin;C:\Program Files\Tools;`
	const directory = `C:\Users\A User\AppData\Local\NeuxBot\bin`
	got, changed := appendWindowsPath(original, directory, true)
	if !changed || got != `%USERPROFILE%\bin;C:\Program Files\Tools;;C:\Users\A User\AppData\Local\NeuxBot\bin` {
		t.Fatalf("user PATH entries changed: %q (%v)", got, changed)
	}
}

func TestAppendWindowsPathRecognizesExistingExpandedDirectory(t *testing.T) {
	t.Setenv("LOCALAPPDATA", `C:\Users\A User\AppData\Local`)
	const original = `C:\custom;"%LOCALAPPDATA%\NEUXBOT\BIN\"`
	got, changed := appendWindowsPath(original, `C:\users\A User\AppData\Local\NeuxBot\bin`, true)
	if changed || got != original {
		t.Fatalf("existing user PATH entry was duplicated: %q (%v)", got, changed)
	}
}
