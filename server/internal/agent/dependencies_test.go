package agent_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestAgentDependencyBoundaries 保护产品会话与原生接入的单向依赖，并拒绝旧分层重新出现。
func TestAgentDependencyBoundaries(t *testing.T) {
	const module = "github.com/JieWaZi/NeuxBot/server/"
	for _, name := range []string{"core", "host", "environment", "../agentcapability", "../agentcli", "../agentcommand"} {
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Errorf("obsolete agent package still exists: %s (%v)", name, err)
		}
	}
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == "core" || entry.Name() == "host" || entry.Name() == "environment" {
				return filepath.SkipDir
			}
			if entry.Name() == "internal" {
				t.Errorf("nested internal boundary reintroduced: %s", path)
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, item := range file.Imports {
			dependency, err := strconv.Unquote(item.Path.Value)
			if err != nil {
				return err
			}
			local := strings.TrimPrefix(dependency, module)
			if strings.HasPrefix(path, "native"+string(filepath.Separator)) && strings.HasPrefix(dependency, module) {
				switch local {
				case "internal/accountconfig", "internal/codexoauth", "internal/logx":
				default:
					if local != "internal/agent/native" {
						t.Errorf("%s: native imports product owner %s", path, local)
					}
				}
			}
			for _, forbidden := range []string{"apis", "services", "repositories"} {
				if local == forbidden || strings.HasPrefix(local, forbidden+"/") {
					t.Errorf("%s: agent imports business or persistence layer %s", path, local)
				}
			}
			if strings.HasPrefix(dependency, "gorm.io/") {
				t.Errorf("%s: agent imports persistence implementation %s", path, dependency)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
