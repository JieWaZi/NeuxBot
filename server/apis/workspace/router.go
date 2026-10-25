// Package workspace 提供绑定认证主体和 Chat 冻结范围的文件接口。
package workspace

import "github.com/shrewx/ginx"

// Router 注册工作范围与文件操作。
var Router = ginx.NewRouter(ginx.Group("/workspace"))

func init() {
	Router.Register(&BodyLimit{})
	Router.Register(&ListChatFiles{})
	Router.Register(&DeleteChatFile{})
	Router.Register(&OpenDraftScope{})
	Router.Register(&RenameDirectory{})
	Router.Register(&DeleteDirectory{})
	Router.Register(&ImportFile{})
	Router.Register(&ResolveReference{})

	Router.Register(&GitStatus{})
	Router.Register(&GitLog{})
	Router.Register(&GitDiff{})
	Router.Register(&GitShow{})
	Router.Register(&GitCommitDiff{})
	Router.Register(&GitBranches{})
	Router.Register(&GitCheckoutPreview{})
	Router.Register(&GitStage{})
	Router.Register(&GitUnstage{})
	Router.Register(&GitDiscard{})
	Router.Register(&GitCommit{})
	Router.Register(&GitCheckout{})

	Router.Register(&OpenScope{})
	Router.Register(&ListDirectory{})
	Router.Register(&SearchFiles{})
	Router.Register(&StatFile{})
	Router.Register(&ReadFile{})
	Router.Register(&WriteFile{})
	Router.Register(&RenameFile{})
	Router.Register(&DeleteFile{})
}
