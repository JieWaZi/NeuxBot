package memory

import (
	"context"
	"errors"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/statuserror"
)

// WakerReader 是范围属主校验需要的最小伙伴查询端口。
type WakerReader interface {
	// Get 按标识读取伙伴。
	Get(ctx context.Context, id string) (models.Waker, error)
}

// ProjectReader 是范围属主校验需要的最小项目查询端口。
type ProjectReader interface {
	// Get 按标识读取项目。
	Get(ctx context.Context, id string) (models.Project, error)
}

// NewScopeExistsResolver 创建供记忆启动清理使用的范围属主查询适配器。
//
// 范围属主指一条记忆归属的伙伴或项目是否仍存在于主业务库；删除伙伴或项目后，
// memory.db 中对应 scope 的记忆需要被清理。
func NewScopeExistsResolver(wakers WakerReader, projects ProjectReader) ScopeExists {
	return func(ctx context.Context, scope constants.MemoryScope, scopeID string) (bool, error) {
		switch scope {
		case constants.MemoryScopePartner:
			_, err := wakers.Get(ctx, scopeID)
			return scopeOwnerExistsUnlessNotFound(err, status_error.WakerNotFound)
		case constants.MemoryScopeProject:
			_, err := projects.Get(ctx, scopeID)
			return scopeOwnerExistsUnlessNotFound(err, status_error.ProjectNotFound)
		default:
			return false, nil
		}
	}
}

func scopeOwnerExistsUnlessNotFound(err error, notFound status_error.StatusError) (bool, error) {
	if err == nil {
		return true, nil
	}
	if errors.Is(err, notFound) {
		return false, nil
	}
	var commonError statuserror.CommonError
	if errors.As(err, &commonError) && commonError.Code() == notFound.Code() {
		return false, nil
	}
	return false, err
}
