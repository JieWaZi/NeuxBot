package builtinwaker

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// ControllerCatalog 使用 Repository Controller 实现内置默认伙伴种子端口。
type ControllerCatalog struct {
	// Wakers 提供伙伴持久化能力。
	Wakers controllers.WakerController
}

// Create 创建伙伴。
func (catalog ControllerCatalog) Create(
	ctx context.Context,
	waker models.Waker,
) error {
	return catalog.Wakers.Create(ctx, waker)
}

// Get 按固定标识读取伙伴。
func (catalog ControllerCatalog) Get(
	ctx context.Context,
	wakerID string,
) (models.Waker, error) {
	return catalog.Wakers.Get(ctx, wakerID)
}
