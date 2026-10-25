// plugin-package 仅在构建时复用正式解析器生成目录元数据，不随桌面发布。
package main

import (
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/spf13/cobra"
	"os"
)

func main() {
	packages := make([]plugins.Package, 0, len(os.Args)-1)
	for _, directory := range os.Args[1:] {
		pkg, err := plugins.Load(directory)
		if err != nil {
			logx.Errorf("生成插件目录失败：%v", err)
			os.Exit(1)
		}
		packages = append(packages, pkg)
	}
	if err := json.NewEncoder((&cobra.Command{}).OutOrStdout()).Encode(packages); err != nil {
		logx.Errorf("生成插件目录失败：%v", err)
		os.Exit(1)
	}
}
