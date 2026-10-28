package runtimes

import (
	"context"

	"errors"
	"path/filepath"

	"github.com/JieWaZi/NeuxBot/server/constants"

	acpcursor "github.com/JieWaZi/acp-go/pkg/cursor"
	acp "github.com/JieWaZi/acp-go/sdk"
)

// cursorACPRuntimeBinding 声明 Cursor 原生工厂与权限目录。
func cursorACPRuntimeBinding() acpRuntimeBinding {
	binding := nativeACPRuntimeBinding()
	binding.baseRuntimeID = RuntimeIDCursor
	binding.displayName = "Cursor"
	binding.permissionLevels = cursorACPPermissionLevels
	binding.stateDirectory = cursorStateDirectory
	binding.newAgent = func(ctx context.Context, config acpAdapterConfig) (acp.Agent, error) {
		return acpcursor.NewAgent(ctx, acpcursor.Config{
			StateDirectory:             config.StateDirectory,
			StateDirectoryForWorkspace: func(cwd string) string { return filepath.Join(filepath.Dir(cwd), "cursor-state") },
			Logger:                     acpAdapterLogger(ctx),
			CursorPath:                 config.Executable,
			PrefixArgs:                 cloneStrings(config.PrefixArgs),
			Environment:                append([]string(nil), config.Environment...),
			WorkingDirectory:           config.WorkingDirectory,
			PermissionMode:             config.PermissionMode,
		})
	}
	return binding
}

// cursorACPFailureCode 优先识别 Cursor 原生错误码，再回退到共享 ACP 分类。
func cursorACPFailureCode(err error) FailureCode {
	var requestError *acp.RequestError
	if errors.As(err, &requestError) {
		metadata, ok := requestError.Data.(map[string]any)
		if ok {
			cursorErrorCode, _ := metadata[constants.RuntimeACPMetaKeyCursorErrorCode].(string)
			if cursorErrorCode == constants.RuntimeACPCursorErrorCodeUpgrade {
				return FailureCodePlanUpgradeRequired
			}
		}
	}
	return acpFailureCode(err)
}

// cursorACPPermissionLevels 仅在 Cursor 声明 agent 模式时公开三档受管权限。
func cursorACPPermissionLevels(options []acp.SessionConfigOption, modes []acp.SessionMode) []PermissionLevel {
	if hasAnyMode(acpAvailableModeIDs(options, modes), "agent") {
		return []PermissionLevel{PermissionLevelDefault, PermissionLevelAuto, PermissionLevelFullAccess}
	}
	return []PermissionLevel{}
}

// cursorStateDirectory 返回 Chat 级 Cursor 持久状态目录，供冷恢复复用 store.db。
func cursorStateDirectory(_ preparedRuntime, workspace Workspace) string {
	if !workspace.Managed {
		return ""
	}
	path := filepath.Clean(workspace.Path)
	if path == "" || path == "." {
		return ""
	}
	return filepath.Join(filepath.Dir(path), "cursor-state")
}
