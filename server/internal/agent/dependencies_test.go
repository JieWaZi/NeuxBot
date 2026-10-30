package agent_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestRuntimeDependencyBoundaries 保护执行内核与环境模块的单向依赖。
func TestRuntimeDependencyBoundaries(t *testing.T) {
	const module = "github.com/JieWaZi/NeuxBot/server/"
	for _, root := range []string{"core", "environment"} {
		t.Run(root, func(t *testing.T) {
			err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
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
					if !strings.HasPrefix(dependency, module) {
						continue
					}
					local := strings.TrimPrefix(dependency, module)
					if root == "core" && !strings.HasPrefix(local, "internal/agent/core/") {
						t.Errorf("%s: core imports product package %s", path, local)
					}
					if root == "environment" {
						for _, forbidden := range []string{"internal/agent/host", "apis", "services", "repositories"} {
							if local == forbidden || strings.HasPrefix(local, forbidden+"/") {
								t.Errorf("%s: environment imports business owner %s", path, local)
							}
						}
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
