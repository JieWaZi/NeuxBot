// Package workspace_attention 提供本机关注事项查询。
package workspace_attention

import "github.com/shrewx/ginx"

// Router 注册本机关注事项查询接口。
var Router = ginx.NewRouter(ginx.Group("/workspace-attention"))

func init() { Router.Register(&GetWorkspaceAttention{}); Router.Register(&GetExecutorAttention{}) }
