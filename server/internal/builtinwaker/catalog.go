// Package builtinwaker 在首次启动时写入内置默认伙伴。
package builtinwaker

import (
	"context"
	"errors"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/statuserror"
)

// Catalog 是内置默认伙伴种子所需的最小持久化端口。
type Catalog interface {
	// Create 创建伙伴。
	Create(ctx context.Context, waker models.Waker) error
	// Get 按固定标识读取伙伴。
	Get(ctx context.Context, wakerID string) (models.Waker, error)
}

// EnsureDefaults 幂等写入全部配置了 DefaultWaker 的内置伙伴。
func EnsureDefaults(ctx context.Context, wakers Catalog) error {
	for _, definition := range builtinrolepreset.DefinitionsWithDefaultWaker() {
		if err := EnsureDefinition(ctx, wakers, definition); err != nil {
			return err
		}
	}
	return nil
}

// EnsureDefinition 为工作入口与启动初始化创建同一份内置伙伴，保留现有用户资料。
func EnsureDefinition(
	ctx context.Context,
	wakers Catalog,
	definition builtinrolepreset.Definition,
) error {
	seed := definition.DefaultWaker
	if seed == nil {
		return nil
	}
	if _, err := wakers.Get(ctx, seed.ID); err == nil {
		return nil
	} else if !hasStatusCode(err, status_error.WakerNotFound) {
		return err
	}
	projection := definition.DisplayProjection()
	now := time.Now().UnixMilli()
	waker := models.Waker{
		ID:          seed.ID,
		Name:        seed.Name,
		RoleName:    definition.RoleName,
		Avatar:      definition.DefaultAvatar,
		Bio:         definition.Description,
		Expertise:   projection.Expertise,
		WorkStyle:   projection.WorkStyle,
		WorkMethods: projection.WorkMethods,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := wakers.Create(ctx, waker); err != nil {
		if hasStatusCode(err, status_error.WakerNameConflict) {
			return nil
		}
		return err
	}
	return nil
}

func hasStatusCode(err error, expected status_error.StatusError) bool {
	if errors.Is(err, expected) {
		return true
	}
	var commonError statuserror.CommonError
	return errors.As(err, &commonError) && commonError.Code() == expected.Code()
}
