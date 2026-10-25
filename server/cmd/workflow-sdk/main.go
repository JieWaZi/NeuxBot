// workflow-sdk 从唯一 registry 生成创作知识与类型，并支持逐字节检查。
package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"sort"

	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
)

func main() {
	check := flag.Bool("check", false, "compare generated files without changing them")
	output := flag.String("output", "internal/workflow/dsl/generated", "generated artifact directory")
	flag.Parse()
	outputs := workflowdsl.SDKOutputs()
	names := []string{}
	for name := range outputs {
		names = append(names, name)
	}
	sort.Strings(names)
	if !*check {
		if err := os.MkdirAll(*output, 0o755); err != nil {
			logx.Errorf("create SDK artifact directory failed: error=%v", err)
			os.Exit(1)
		}
	}
	for _, name := range names {
		path := filepath.Join(*output, name)
		if *check {
			actual, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(actual, outputs[name]) {
				logx.Errorf("workflow SDK artifact differs: path=%s error=%v", path, err)
				os.Exit(1)
			}
		} else {
			if err := os.WriteFile(path, outputs[name], 0o644); err != nil {
				logx.Errorf("write SDK artifact failed: path=%s error=%v", path, err)
				os.Exit(1)
			}
		}
	}
	// 输出目录由生成器独占；契约删除或改名后不能继续打包失效资源。
	entries, err := os.ReadDir(*output)
	if err != nil {
		logx.Errorf("read SDK artifact directory failed: error=%v", err)
		os.Exit(1)
	}
	for _, entry := range entries {
		if _, current := outputs[entry.Name()]; current {
			continue
		}
		path := filepath.Join(*output, entry.Name())
		if *check || entry.IsDir() {
			logx.Errorf("unexpected workflow SDK artifact: path=%s", path)
			os.Exit(1)
		}
		if err := os.Remove(path); err != nil {
			logx.Errorf("remove stale SDK artifact failed: path=%s error=%v", path, err)
			os.Exit(1)
		}
	}
	logx.Info("workflow SDK artifacts verified")
}
