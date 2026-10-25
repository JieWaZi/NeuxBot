package runtimes

import (
	"slices"
	"strings"
)

// protectedPrefixArguments 禁止前置参数覆盖 NeuxBot 管理的原生执行设置。
var protectedPrefixArguments = map[string]map[string]struct{}{
	RuntimeIDQoder:     {"--": {}, "--acp": {}, "--cwd": {}, "-w": {}, "--config-dir": {}, "--add-dir": {}, "--attachment": {}, "--worktree": {}, "--model": {}, "-m": {}, "--reasoning-effort": {}, "--context-window": {}, "--list-models": {}, "--continue": {}, "-c": {}, "--resume": {}, "-r": {}, "--session-id": {}, "--fork-session": {}, "--list-sessions": {}, "--delete-session": {}, "--print": {}, "-p": {}, "--output-format": {}, "-o": {}, "--input-format": {}, "--prompt-interactive": {}, "-i": {}, "--no-session-persistence": {}, "--permission-mode": {}, "--yolo": {}, "--dangerously-skip-permissions": {}, "--tools": {}, "--allowed-tools": {}, "--disallowed-tools": {}, "--mcp-config": {}, "--strict-mcp-config": {}, "--allowed-mcp-server-names": {}, "--agent": {}, "--agents": {}, "--system-prompt": {}, "--append-system-prompt": {}, "--settings": {}, "--setting-sources": {}, "--plugin-dir": {}, "--remote": {}, "--remote-session": {}},
	RuntimeIDWorkBuddy: {"--": {}, "--acp": {}, "--acp-transport": {}, "--serve": {}, "--print": {}, "-p": {}, "--input-format": {}, "--output-format": {}, "--model": {}, "--effort": {}, "--permission-mode": {}, "--permission-mode-before-plan": {}, "--subagent-permission-mode": {}, "--dangerously-skip-permissions": {}, "-y": {}, "--resume": {}, "-r": {}, "--continue": {}, "-c": {}, "--session-id": {}, "--no-session-persistence": {}, "--fork-session": {}, "--add-dir": {}, "--mcp-config": {}, "--strict-mcp-config": {}, "--system-prompt": {}, "--system-prompt-file": {}, "--append-system-prompt": {}, "--agent": {}, "--multitask": {}, "--sandbox": {}, "--tools": {}, "--allowedTools": {}, "--disallowedTools": {}},
	RuntimeIDOpenCode:  {"acp": {}, "--": {}, "--model": {}, "-m": {}, "--agent": {}, "--session": {}, "-s": {}, "--continue": {}, "-c": {}, "--dir": {}, "--port": {}, "--hostname": {}},
	RuntimeIDGemini:    {"--": {}, "--acp": {}, "--experimental-acp": {}, "--model": {}, "-m": {}, "--prompt": {}, "-p": {}, "--approval-mode": {}, "--yolo": {}, "-y": {}, "--resume": {}, "-r": {}, "--sandbox": {}, "-s": {}, "--include-directories": {}, "--allowed-tools": {}, "--allowed-mcp-server-names": {}, "--output-format": {}},
	RuntimeIDGrok:      {"acp": {}, "--": {}, "--model": {}, "-m": {}, "--prompt": {}, "-p": {}, "--config": {}, "-c": {}, "--session": {}, "--resume": {}, "--yolo": {}, "--approval-mode": {}, "--directory": {}, "--cwd": {}},
	RuntimeIDKimi:      {"acp": {}, "--": {}, "--print": {}, "-p": {}, "--model": {}, "-m": {}, "--thinking": {}, "--no-thinking": {}, "--yolo": {}, "-y": {}, "--auto": {}, "--mode": {}, "--work-dir": {}, "-w": {}, "--session": {}, "--continue": {}, "--mcp-config": {}},
	RuntimeIDCursor:    {"acp": {}, "--": {}, "--print": {}, "-p": {}, "--model": {}, "-m": {}, "--mode": {}, "--force": {}, "-f": {}, "--yolo": {}, "--resume": {}, "--workspace": {}, "--output-format": {}, "--stream-partial-output": {}, "--auto-review": {}, "--approve-mcps": {}, "--sandbox": {}, "--trust": {}},
	RuntimeIDPi:        {"--": {}, "--mode": {}, "--model": {}, "--provider": {}, "--session": {}, "--session-dir": {}, "--thinking": {}, "--no-session": {}, "--extension": {}, "-e": {}, "--no-extensions": {}},
	RuntimeIDCodex: {
		"-C":                    {},
		"--cd":                  {},
		"--add-dir":             {},
		"--sandbox":             {},
		"-s":                    {},
		"--model":               {},
		"-m":                    {},
		"--image":               {},
		"-i":                    {},
		"-c":                    {},
		"--config":              {},
		"--json":                {},
		"--skip-git-repo-check": {},
		"--ask-for-approval":    {},
		"-a":                    {},
		"--dangerously-bypass-approvals-and-sandbox": {},
		"exec":       {},
		"resume":     {},
		"app-server": {},
	},
	RuntimeIDClaude: {
		"-p":                             {},
		"--print":                        {},
		"--input-format":                 {},
		"--output-format":                {},
		"--verbose":                      {},
		"--plugin-dir":                   {},
		"--include-partial-messages":     {},
		"--model":                        {},
		"--add-dir":                      {},
		"--resume":                       {},
		"--session-id":                   {},
		"--permission-mode":              {},
		"--dangerously-skip-permissions": {},
		"--allowedTools":                 {},
		"--disallowedTools":              {},
	},
}

// LaunchOverrideArgumentsAllowed 判断前置参数是否避开 NeuxBot 管理的协议、模型、Session、工作区和权限参数。
func LaunchOverrideArgumentsAllowed(baseRuntimeID string, arguments []string) bool {
	baseRuntimeID = strings.TrimSpace(baseRuntimeID)
	if baseRuntimeID == RuntimeIDQoderCN {
		baseRuntimeID = RuntimeIDQoder
	}
	protected, exists := protectedPrefixArguments[baseRuntimeID]
	if !exists {
		return false
	}
	for _, argument := range arguments {
		name := strings.TrimSpace(argument)
		if flag, _, found := strings.Cut(name, "="); found {
			name = flag
		}
		if _, blocked := protected[name]; blocked {
			return false
		}
	}
	return true
}

// LaunchDetectInputsEqual 判断两次环境配置是否会形成相同的 ACP 探测启动输入。
func LaunchDetectInputsEqual(previous, next LaunchDetectInput) bool {
	return strings.TrimSpace(previous.Binary) == strings.TrimSpace(next.Binary) &&
		strings.TrimSpace(previous.ConfigHome) == strings.TrimSpace(next.ConfigHome) &&
		slices.Equal(previous.PrefixArgs, next.PrefixArgs)
}

// LaunchDetectInput 保存会影响 Runtime 探测的受控启动字段。
type LaunchDetectInput struct {
	// Binary 是用户保存的可执行文件名或绝对路径。
	Binary string
	// PrefixArgs 是放在 Runtime 子命令之前的用户前置参数。
	PrefixArgs []string
	// ConfigHome 是由 Runtime Definition 映射到受控环境变量的配置主目录。
	ConfigHome string
}
