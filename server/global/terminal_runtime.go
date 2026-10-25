package global

import "github.com/JieWaZi/NeuxBot/server/internal/terminalruntime"

// TerminalRuntime 由 Core 持有交互终端子进程，不拥有业务 Execution。
var TerminalRuntime *terminalruntime.Supervisor
