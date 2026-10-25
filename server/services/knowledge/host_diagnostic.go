package knowledge

import (
	"context"
	"errors"
	"strconv"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// hostDiagnostic 映射已知稳定码，不存储可能带账号、路径和来源正文的任意错误文字。
func hostDiagnostic(err error) (string, string) {
	code, message := "unknown", "知识整理未完成，请检查模型服务后重试。"
	if errors.Is(err, context.Canceled) {
		return "interrupted", "宿主已停止等待，恢复时将先核对知识任务检查点。"
	}
	var coded interface{ Code() int64 }
	if errors.As(err, &coded) {
		code = strconv.FormatInt(coded.Code(), 10)
		switch coded.Code() {
		case status_error.KnowledgeForbidden.Code():
			message = "当前授权已失效，请检查知识库授权后重试。"
		case status_error.KnowledgeUnsupported.Code():
			message = "当前模型配置不支持此操作，请检查配置。"
		case status_error.BadRequest.Code(), status_error.KnowledgeInvalid.Code():
			message = "模型请求或配置不符合要求，请检查配置后重试。"
		}
	}
	return code, message
}
