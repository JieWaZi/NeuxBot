package catalog

import (
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"

	"path/filepath"

	acp "github.com/JieWaZi/acp-go/sdk"
)

// cursorACPRuntimeBinding 声明 Cursor 原生工厂与权限目录。
func cursorACPRuntimeBinding() acpRuntimeBinding {
	binding := nativeACPRuntimeBinding()
	binding.baseRuntimeID = runtimecontract.RuntimeIDCursor
	binding.displayName = "Cursor"
	binding.permissionLevels = cursorACPPermissionLevels
	binding.stateDirectory = cursorStateDirectory

	return binding
}

// cursorACPPermissionLevels 仅在 Cursor 声明 agent 模式时公开三档受管权限。
func cursorACPPermissionLevels(options []acp.SessionConfigOption, modes []acp.SessionMode) []runtimecontract.PermissionLevel {
	if hasAnyMode(acpAvailableModeIDs(options, modes), "agent") {
		return []runtimecontract.PermissionLevel{runtimecontract.PermissionLevelDefault, runtimecontract.PermissionLevelAuto, runtimecontract.PermissionLevelFullAccess}
	}
	return []runtimecontract.PermissionLevel{}
}

// cursorStateDirectory 返回 Chat 级 Cursor 持久状态目录，供冷恢复复用 store.db。
func cursorStateDirectory(_ preparedRuntime, workspace runtimecontract.Workspace) string {
	if !workspace.Managed {
		return ""
	}
	path := filepath.Clean(workspace.Path)
	if path == "" || path == "." {
		return ""
	}
	return filepath.Join(filepath.Dir(path), "cursor-state")
}
