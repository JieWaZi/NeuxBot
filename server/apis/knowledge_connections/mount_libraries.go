package knowledge_connections

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
)

// offlineMountedLibrary 使用已验证的本机连接快照保留读取失败远端的入口。
func offlineMountedLibrary(c *remoteClients, libraryID string) knowledge.Library {
	value := c.connection.Detection.Library
	if value.ID != libraryID {
		value.ID = libraryID
		value.Name = libraryID
	}
	row := officialLibrary(c, value)
	row.Offline = true
	return row
}

// mountedLibraries 只读取挂载目标详情，不调用远端的空间目录。
func mountedLibraries(ctx context.Context, c *remoteClients) ([]knowledge.Library, error) {
	if global.KnowledgeRemoteMountControllerManager == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	mounts, err := global.KnowledgeRemoteMountControllerManager.List(ctx, c.connection.ID)
	if err != nil {
		return nil, err
	}
	items := []knowledge.Library{}
	for _, mount := range mounts {
		// 入口始终由本机挂载决定；只有成功读取远端详情才覆盖离线快照。
		row := offlineMountedLibrary(c, mount.KnowledgeBaseID)
		if c.custom != nil {
			// 挂载列表已核对目标，复用本次连接快照，不逐库重新读取凭据和挂载。
			target, err := clientSnapshot(c.connection, c.options.Token, mount.KnowledgeBaseID)
			if err == nil {
				value, err := target.custom.GetLibrary(ctx)
				if err == nil {
					row = customLibrary(target, value)
				}
			}
		} else if c.official != nil {
			value, err := c.official.GetLibrary(ctx, mount.KnowledgeBaseID)
			if err == nil {
				row = officialLibrary(c, *value)
			}
		}
		row.MountID, row.MountRevision = mount.ID, mount.Revision
		items = append(items, row)
	}
	return items, nil
}
