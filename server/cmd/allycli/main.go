// allycli 是随 NeuxBot 应用交付的受管能力命令入口。
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	agentcli "github.com/JieWaZi/NeuxBot/server/internal/agent/cli"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := agentcli.Execute(ctx, os.Args[1:], agentcli.IO{})
	cancel()
	os.Exit(code)
}
