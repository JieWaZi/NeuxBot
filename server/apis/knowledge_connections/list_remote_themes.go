package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取远端主题
type ListRemoteThemes struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 完整目录
	All bool `in:"query" name:"all"`
	// 上级标识
	ParentID string `in:"query" name:"parentId"`
}

func (o *ListRemoteThemes) Path() string { return "/:connectionId/libraries/:libraryId/themes" }
func (o *ListRemoteThemes) Output(ctx *gin.Context) (result interface{}, err error) {
	requestCtx := ctx.Request.Context()
	defer func() {
		if requestCtx.Err() != nil {
			result = nil
			err = status_error.KnowledgeCancelled
		}
	}()
	if requestCtx.Err() != nil {
		return nil, status_error.KnowledgeCancelled
	}
	c, err := client(requestCtx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	out := RemoteThemeList{ParentID: o.ParentID, Items: []RemoteTheme{}}
	if c.custom != nil {
		rows, e := c.custom.ListThemes(requestCtx)
		if e != nil {
			return nil, remoteError(e)
		}
		if !remoteThemesValid(rows) {
			return nil, status_error.KnowledgeRemoteIncompatible
		}
		out.Complete = o.All
		parents := make(map[string]bool, len(rows))
		for _, row := range rows {
			parents[row.ParentID] = true
		}
		for _, v := range rows {
			if !o.All && v.ParentID != o.ParentID {
				continue
			}
			out.Items = append(out.Items, RemoteTheme{Theme: knowledge.Theme{ID: v.ID, ParentID: v.ParentID, Name: v.Name, Path: v.Path, SortOrder: v.SortOrder, Manual: v.Manual, SystemKey: v.SystemKey}, HasChildren: parents[v.ID]})
		}
		return out, nil
	}
	v, e := c.official.ListFolders(requestCtx, o.LibraryID, o.ParentID)
	if e != nil {
		return nil, remoteError(e)
	}
	for _, x := range v.Folders {
		out.Items = append(out.Items, RemoteTheme{Theme: knowledge.Theme{ID: x.ID, ParentID: x.ParentID, Name: x.Name, Path: x.Path, SortOrder: x.SortOrder}, HasChildren: x.HasChildren, PageCount: x.PageCount})
	}
	return out, nil
}

// remoteThemesValid 校验完整目录表，避免根级过滤把孤立节点或不可达环伪装为空目录。
func remoteThemesValid(rows []*core.Theme) bool {
	parents := make(map[string]string, len(rows))
	for _, row := range rows {
		if row == nil || row.ID == "" {
			return false
		}
		if _, exists := parents[row.ID]; exists {
			return false
		}
		parents[row.ID] = row.ParentID
	}
	for _, parent := range parents {
		if parent != "" {
			if _, exists := parents[parent]; !exists {
				return false
			}
		}
	}
	states := make(map[string]uint8, len(parents))
	for id := range parents {
		path := []string{}
		current := id
		for current != "" && states[current] == 0 {
			states[current] = 1
			path = append(path, current)
			current = parents[current]
		}
		if current != "" && states[current] == 1 {
			return false
		}
		for _, visited := range path {
			states[visited] = 2
		}
	}
	return true
}
