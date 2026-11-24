package memory

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// WakerReader 读取伙伴归属，供记忆存储清理孤儿记录。
type WakerReader interface {
	// Get 返回当前伙伴或 WakerNotFound。
	Get(ctx context.Context, id string) (models.Waker, error)
}

// NewPartnerExistsResolver 将伙伴查找映射为存在性，保留数据库错误。
func NewPartnerExistsResolver(wakers WakerReader) PartnerExists {
	return func(ctx context.Context, partnerID string) (bool, error) {
		_, err := wakers.Get(ctx, partnerID)
		if util.HasStatusCode(err, status_error.WakerNotFound) {
			return false, nil
		}
		return err == nil, err
	}
}
