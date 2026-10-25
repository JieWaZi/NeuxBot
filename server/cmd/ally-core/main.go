// NeuxBot Core 是伙伴工作台的本地业务与持久化进程。
package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/JieWaZi/NeuxBot/server/apis"
	"github.com/JieWaZi/NeuxBot/server/apis/integrations"
	apiMiddleware "github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/localweb"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/parentwatch"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/stdio"
	"github.com/JieWaZi/NeuxBot/server/internal/terminalruntime"
	"github.com/JieWaZi/NeuxBot/server/internal/uploads"
	workflowfunction "github.com/JieWaZi/NeuxBot/server/internal/workflow/function"
	"github.com/shrewx/ginx"
	"github.com/spf13/cobra"
)

//go:generate toolx gen openapi

func main() {
	if len(os.Args) > 1 && os.Args[1] == constants.PluginStdioMode {
		streams := &cobra.Command{}
		os.Exit(stdio.Main(os.Args[2:], streams.InOrStdin(), streams.OutOrStdout(), streams.ErrOrStderr()))
	}
	if workflowfunction.IsChildMode() {
		os.Exit(workflowfunction.RunChild(os.Stdin))
	}
	ginx.Launch(func(command *cobra.Command, _ []string) {
		ginx.Parse(global.Config)
		ginx.SetCORSMiddleware(apiMiddleware.CORS())
		if err := global.Prepare(global.Config); err != nil {
			panic(err)
		}

		unlock, err := localweb.LockDataDirectory(global.Config.DataDirectory)
		if err != nil {
			panic(err)
		}
		defer unlock()
		global.Uploads, err = uploads.New(global.Config.DataDirectory)
		if err != nil {
			panic(err)
		}
		defer global.Uploads.Close()
		if directory, developmentOrigin := os.Getenv("ALLY_WEB_DIR"), os.Getenv("ALLY_WEB_DEV_ORIGIN"); directory != "" || developmentOrigin != "" {
			if global.Config.Server.Https {
				panic("本机 Web 模式使用回环 HTTP")
			}
			host := net.JoinHostPort(global.Config.Server.Host, strconv.Itoa(global.Config.Server.Port))
			global.LocalWeb, err = localweb.New(host, directory, developmentOrigin)
			if err != nil {
				panic(err)
			}
			defer global.LocalWeb.Close()
			emitPairing := func() {
				code, err := global.LocalWeb.RotatePairing()
				if err != nil {
					logx.Errorf("issue Web pairing code failed: %v", err)
					return
				}
				origin := "http://" + host
				if developmentOrigin != "" {
					origin = developmentOrigin
				}
				// 配对凭据属于交互命令输出，不进入持久化服务日志。
				fmt.Fprintf(command.OutOrStdout(), "NeuxBot Web: %s\n一次性配对码（5 分钟有效）：%s\n在此终端输入 pair 并回车可重新签发。\n", origin, code)
			}
			emitPairing()
			go func() {
				scanner := bufio.NewScanner(command.InOrStdin())
				for scanner.Scan() {
					if strings.TrimSpace(scanner.Text()) == "pair" {
						emitPairing()
					}
				}
			}()
		}
		rootContext, cancel := context.WithCancel(context.Background())
		var parentWaitGroup sync.WaitGroup
		if parentPIDValue := os.Getenv("ALLY_DESKTOP_PARENT_PID"); parentPIDValue != "" {
			parentPID, err := strconv.Atoi(parentPIDValue)
			if err != nil || parentPID <= 0 {
				cancel()
				panic("ALLY_DESKTOP_PARENT_PID 必须是有效进程标识")
			}
			parentWaitGroup.Add(1)
			go func() {
				defer parentWaitGroup.Done()
				if !parentwatch.Wait(rootContext, parentPID, 100*time.Millisecond) {
					return
				}
				process, findErr := os.FindProcess(os.Getpid())
				if findErr == nil {
					_ = process.Signal(syscall.SIGTERM)
				}
			}()
		}
		if err := global.Load(rootContext, global.Config, integrations.NewPublishedHandler); err != nil {
			cancel()
			parentWaitGroup.Wait()
			panic(err)
		}

		if worker := os.Getenv("ALLY_TERMINAL_RUNTIME_PATH"); worker != "" {
			global.TerminalRuntime, err = terminalruntime.New(os.Getenv("ALLY_TERMINAL_NODE_PATH"), worker)
			if err != nil {
				logx.Errorf("terminal runtime unavailable: %v", err)
			} else {
				defer global.TerminalRuntime.Close()
			}
		}
		closeCore := onceCleanup(func() {
			cancel()
			parentWaitGroup.Wait()
			if err := global.Close(); err != nil {
				logx.Errorf("close NeuxBot Core failed: %v", err)
			}
		})
		// ginx 的 Shutdown hook 异步运行；退出仍须等待同一关闭完成，再回收终端和释放数据锁。
		defer closeCore()
		ginx.AddShutdownHook(closeCore)
		ginx.RunServer(&global.Config.Server, apis.RootRouter)
	})
}

// onceCleanup 让异步关闭钩子与主函数退出等待同一次资源回收。
func onceCleanup(cleanup func()) func() {
	var once sync.Once
	return func() { once.Do(cleanup) }
}
