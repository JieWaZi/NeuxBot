package agent

import (
	"context"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// Read fresh Machine and User PATH entries as T3 Code does. Windows expands
// registry variables in the new environment block instead of using stale process values.
func readHostExecutablePath(ctx context.Context, _ string, _ environmentSnapshot) ([]string, bool) {
	if ctx.Err() != nil {
		return nil, false
	}
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY|windows.TOKEN_DUPLICATE, &token); err != nil {
		return nil, false
	}
	defer token.Close()
	environment, err := token.Environ(false)
	if err != nil {
		return nil, false
	}
	for _, entry := range environment {
		name, value, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, "PATH") {
			return filepath.SplitList(value), true
		}
	}
	return nil, true
}
