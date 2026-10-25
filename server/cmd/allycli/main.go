// allycli 是随 NeuxBot 应用交付的受管能力命令入口。
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcli"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := agentcli.Execute(ctx, os.Args[1:], agentcli.IO{ContextFile: os.Getenv(constants.AgentCLIContextEnvironment)})
	cancel()
	os.Exit(code)
}
