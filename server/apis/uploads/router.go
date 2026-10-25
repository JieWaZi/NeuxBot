// Package uploads 将 HTTP 字节流接入受管上传，不接收本机写入路径。
package uploads

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// Router 继承管理认证和写入 CSRF 校验。
var Router = ginx.NewRouter(ginx.Group("/uploads"))

func init() { Router.Register(&StageUpload{}); Router.Register(&DeleteUpload{}) }

// Claim 为真实接纳入口解析主体上传引用；失败释放租约以便重试。
func Claim(ctx *gin.Context, refs []string, requestKeys ...string) ([]string, func(bool), error) {
	if global.Uploads == nil {
		for _, ref := range refs {
			if len(ref) >= 7 && ref[:7] == "upload:" {
				return nil, nil, status_error.BadRequest
			}
		}
		if len(refs) > 0 && len(middleware.AuthSubject(ctx)) >= 4 && middleware.AuthSubject(ctx)[:4] == "web:" {
			return nil, nil, status_error.BadRequest
		}
		return refs, func(bool) {}, nil
	}
	paths, release, err := global.Uploads.Claim(middleware.AuthSubject(ctx), refs, requestKeys...)
	if err != nil {
		return nil, nil, status_error.BadRequest
	}
	return paths, release, nil
}

// ClaimWork 将待接纳消息的上传引用解析为原执行管线可复制的受管字节。
func ClaimWork(ctx *gin.Context, input *execution.WorkInput, requestKeys ...string) (func(bool), error) {
	refs := make([]string, len(input.Attachments))
	for index, item := range input.Attachments {
		refs[index] = item.SourcePath
	}
	if len(requestKeys) == 0 && input.SubmissionID != "" {
		requestKeys = []string{ctx.Request.URL.Path + ":" + input.SubmissionID}
	}
	paths, release, err := Claim(ctx, refs, requestKeys...)
	if err != nil {
		return nil, err
	}
	for index, path := range paths {
		input.Attachments[index].SourcePath = path
	}
	return release, nil
}
