package knowledge_connections

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/remote"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取可用知识库目录
type ListAvailableLibraries struct {
	ginx.MethodGet
	// 保存位置
	Location constants.KnowledgeLibraryLocation `in:"query" name:"location"`
	// 搜索内容
	Search string `in:"query" name:"search" validate:"max=200"`
	// 起始位置
	Offset int `in:"query" name:"offset" validate:"min=0"`
	// 每页条数
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
}

// catalogSource 保存单次请求观察到的来源总数，不作为持久聚合状态。
type catalogSource struct {
	// remote 为空时表示本地引擎，否则固定本次读取连接及凭据。
	remote *remoteClients
	// total 是在线目录或离线已知入口的总数。
	total int64
	// mounted 保存明确挂载或故障降级后的已知库元数据。
	mounted []knowledge.Library
	// official 保存官方无分页接口本次返回的目录元数据，仅用于窗口切片。
	official []remote.Library
}

// Path 返回已认证控制面的跨来源目录入口。
func (o *ListAvailableLibraries) Path() string { return "/libraries" }

// Output 先观察真实总量，再只读取与全局分页重叠的来源窗口。
func (o *ListAvailableLibraries) Output(ctx *gin.Context) (interface{}, error) {
	requestContext := context.Context(ctx)
	if ctx.Request != nil {
		requestContext = ctx.Request.Context()
	}
	offset, limit := remotePagination(knowledge.Pagination{Offset: o.Offset, Limit: o.Limit})
	sources := []catalogSource{}
	if o.Location != constants.KnowledgeLibraryLocationRemote {
		if global.KnowledgeEngine == nil {
			return nil, status_error.KnowledgeUnsupported
		}
		_, total, err := global.KnowledgeEngine.ListLibraries(requestContext, 0, 1)
		if err != nil {
			return nil, bridge.MapError(err)
		}
		sources = append(sources, catalogSource{total: total})
	}
	if o.Location != constants.KnowledgeLibraryLocationLocal {
		if global.KnowledgeConnectionControllerManager == nil {
			return nil, status_error.KnowledgeUnsupported
		}
		connections, err := global.KnowledgeConnectionControllerManager.List(requestContext)
		if err != nil {
			return nil, err
		}
		sort.Slice(connections, func(i, j int) bool { return connections[i].ID < connections[j].ID })
		for _, connection := range connections {
			if !connection.Enabled {
				continue
			}
			c, err := client(requestContext, connection.ID, "")
			if errors.Is(err, status_error.DatabaseError) || errors.Is(err, status_error.DatabaseBusy) {
				return nil, err
			}
			if err != nil {
				// 凭据或协议失效时仍保留已验证的本机入口。
				c = &remoteClients{connection: connection}
			}
			source := catalogSource{remote: c}
			if c.connection.Backend == "integration" {
				source.mounted, err = mountedLibraries(requestContext, c)
				if err != nil {
					return nil, err
				}
				source.total = int64(len(source.mounted))
			} else if err != nil {
				source.mounted = offlineCatalogLibraries(c)
				source.total = int64(len(source.mounted))
			} else if c.custom != nil {
				page, err := c.custom.ListLibraries(requestContext, 0, 1)
				if err != nil {
					source.mounted = offlineCatalogLibraries(c)
					source.total = int64(len(source.mounted))
				} else {
					source.total = page.Total
				}
			} else {
				// 官方协议仅提供无分页目录接口，沿用既有元数据读取边界。
				source.official, err = c.official.ListLibraries(requestContext)
				if err != nil {
					source.mounted = offlineCatalogLibraries(c)
					source.total = int64(len(source.mounted))
				} else {
					source.total = int64(len(source.official))
				}
			}
			sources = append(sources, source)
		}
	}
	if search := strings.ToLower(strings.TrimSpace(o.Search)); search != "" {
		return searchCatalogLibraries(requestContext, sources, offset, limit, search)
	}
rebuildWindow:
	out := knowledge.LibraryList{Items: []knowledge.Library{}}
	remaining := int64(offset)
	for sourceIndex, source := range sources {
		out.Total += source.total
		if remaining >= source.total {
			remaining -= source.total
			continue
		}
		size := min(limit-len(out.Items), int(source.total-remaining))
		if size <= 0 {
			continue
		}
		var window knowledge.LibraryList
		var err error
		if source.remote == nil {
			window, err = knowledge.ListLocalLibraries(requestContext, int(remaining), size, "")
		} else {
			items := []knowledge.Library{}
			if source.mounted != nil {
				items = append(items, source.mounted[int(remaining):int(remaining)+size]...)
			} else if source.remote.custom != nil {
				page, e := source.remote.custom.ListLibraries(requestContext, int(remaining), size)
				if e != nil {
					sources[sourceIndex].mounted = offlineCatalogLibraries(source.remote)
					sources[sourceIndex].total = int64(len(sources[sourceIndex].mounted))
					// 来源降级后总数与全局偏移都重新计算，不留下缺项或越界分页。
					goto rebuildWindow
				} else {
					for _, v := range page.Items {
						items = append(items, customLibrary(source.remote, v))
					}
				}
			} else {
				for _, v := range source.official[int(remaining) : int(remaining)+size] {
					items = append(items, officialLibrary(source.remote, v))
				}
			}
			window, err = withLibrarySummaries(requestContext, source.remote, items, source.total)
		}
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, window.Items...)
		remaining = 0
	}
	return out, nil
}

// searchCatalogLibraries 先按来源匹配完整目录，再组合全局窗口与真实匹配总数。
func searchCatalogLibraries(ctx context.Context, sources []catalogSource, offset, limit int, search string) (knowledge.LibraryList, error) {
	out := knowledge.LibraryList{Items: []knowledge.Library{}}
	remaining := offset
	for _, source := range sources {
		var window knowledge.LibraryList
		var err error
		if source.remote == nil {
			window, err = knowledge.ListLocalLibraries(ctx, remaining, limit-len(out.Items), search)
		} else {
			window, err = searchRemoteCatalogLibraries(ctx, source, remaining, limit-len(out.Items), search)
		}
		if err != nil {
			return knowledge.LibraryList{}, err
		}
		out.Total += window.Total
		out.Items = append(out.Items, window.Items...)
		remaining = max(0, remaining-int(window.Total))
	}
	return out, nil
}

// searchRemoteCatalogLibraries 对无搜索协议的来源逐批匹配，只保存当前结果窗口。
func searchRemoteCatalogLibraries(ctx context.Context, source catalogSource, offset, limit int, search string) (knowledge.LibraryList, error) {
	selected := []knowledge.Library{}
	var matches int64
	collect := func(row knowledge.Library) {
		if !strings.Contains(strings.ToLower(row.Name+" "+row.Purpose), search) {
			return
		}
		if matches >= int64(offset) && len(selected) < limit {
			selected = append(selected, row)
		}
		matches++
	}
	if source.mounted != nil {
		for _, row := range source.mounted {
			collect(row)
		}
	} else if source.remote.custom != nil {
		for cursor := 0; int64(cursor) < source.total; {
			page, err := source.remote.custom.ListLibraries(ctx, cursor, constants.KnowledgeLibraryScanBatchSize)
			if err != nil || (len(page.Items) == 0 && int64(cursor) < page.Total) {
				if ctx.Err() != nil {
					return knowledge.LibraryList{}, status_error.KnowledgeCancelled
				}
				// 目录中途失败也重新计算已知离线入口，不能混用半份在线结果与总数。
				source.mounted = offlineCatalogLibraries(source.remote)
				return searchRemoteCatalogLibraries(ctx, source, offset, limit, search)
			}
			for _, row := range page.Items {
				collect(customLibrary(source.remote, row))
			}
			cursor += len(page.Items)
			source.total = page.Total
		}
	} else {
		for _, row := range source.official {
			collect(officialLibrary(source.remote, row))
		}
	}
	return withLibrarySummaries(ctx, source.remote, selected, matches)
}

// offlineCatalogLibraries 只返回识别阶段已验证的库，不凭连接名称虚构知识库。
func offlineCatalogLibraries(c *remoteClients) []knowledge.Library {
	items := []knowledge.Library{}
	if id := c.connection.Detection.Library.ID; id != "" {
		items = append(items, offlineMountedLibrary(c, id))
	}
	return items
}
