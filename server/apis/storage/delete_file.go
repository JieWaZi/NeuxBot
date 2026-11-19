package storage

import (
	workspaceapi "github.com/JieWaZi/NeuxBot/server/apis/workspace"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// DeleteStorageFile 删除所选存储文件。
type DeleteStorageFile struct {
	ginx.MethodPost
	// 请求体
	Data DeleteStorageFileInput `in:"body"`
}

// DeleteStorageFileInput 存储文件删除请求。
type DeleteStorageFileInput struct {
	// 对象类型
	Kind string `json:"kind" validate:"required,oneof=wakers chats"`
	// 对象标识
	ID string `json:"id" validate:"required,max=32"`
	// 文件标识
	FileID string `json:"fileId" validate:"required,max=4096"`
	// 文件版本
	Version string `json:"version" validate:"required,max=128"`
}

// Path 返回存储文件删除地址。
func (*DeleteStorageFile) Path() string { return "/files/delete" }

// Output 复核文件归属与版本后删除该条目。
func (d *DeleteStorageFile) Output(ctx *gin.Context) (interface{}, error) {
	input := d.Data
	snapshot, err := global.StorageService.Usage(ctx.Request.Context(), false)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if snapshot.ChatMetadataError != nil {
		return nil, ginx.WithStack(snapshot.ChatMetadataError)
	}
	file, belongs := snapshot.FindUserFile(input.Kind, input.ID, input.FileID)
	if !belongs {
		return nil, ginx.WithStack(status_error.WorkspaceResourceNotFound)
	}
	entry := file.Entry
	if entry.UserFileVersion() != input.Version {
		return nil, ginx.WithStack(status_error.WorkspaceResourceConflict)
	}
	managed := snapshot.Measurement.Roots.Managed
	release, err := chatstorage.AcquireOperation(managed, "waker:"+file.WakerID, entry.ChatID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	defer release()
	if _, err := global.WakerControllerManager.Get(ctx, file.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	if entry.ChatID != "" {
		summary, err := global.ChatService.GetChatSummary(ctx, entry.ChatID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		current := summary.Chat
		if current.WakerID != file.WakerID || current.HiddenFromWakerHistory {
			return nil, ginx.WithStack(status_error.WorkspacePathInvalid)
		}
		if current.ActiveExecutionID != "" {
			return nil, ginx.WithStack(status_error.ChatHasActiveExecution)
		}
	} else {
		active, err := global.ChatControllerManager.HasActiveWakerWork(ctx, file.WakerID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		if active {
			return nil, ginx.WithStack(status_error.WakerHasRunningWork)
		}
	}
	request := ctx.Request.Context()
	if err := global.StorageService.CheckFileIdle(request, file.WakerID, entry.ChatID); err != nil {
		return nil, ginx.WithStack(err)
	}
	var inherited map[string]string
	if file.Legacy {
		inherited, err = global.StorageService.InheritedAttachmentPaths(request, entry.ChatID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
	}
	err = workspaceapi.Files().RemoveMeasuredStorageFile(entry, managed, inherited)
	global.StorageService.Invalidate()
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
