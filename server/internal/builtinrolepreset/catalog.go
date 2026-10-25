// Package builtinrolepreset 统一声明内置角色预设的系统定义与启动种子逻辑。
package builtinrolepreset

import (
	"context"
	"errors"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/statuserror"
)

// Definition 是一个内置角色预设的唯一系统定义。
type Definition struct {
	// ID 是数据库使用的稳定预设标识。
	ID string
	// RoleName 是角色名称并同时作为预设展示名称。
	RoleName string
	// DefaultAvatar 是角色展示与内置默认伙伴初始化使用的形象，普通新建伙伴独立随机。
	DefaultAvatar string
	// Description 是用于预设展示和预填伙伴简介的角色描述。
	Description string
	// Projection 是面向页面展示的摘要；内置角色与 Agent 档案分离时使用。
	Projection profile.Projection
	// Documents 保存三份 Agent 档案的完整 Markdown 内容，不含 NeuxBot 固定展示区域。
	Documents map[constants.ProfileDocumentKind]string
	// DefaultSkills 是从该角色新建伙伴及初始化其内置伙伴时自动启用的技能声明名。
	DefaultSkills []string
	// DefaultWaker 是空库 bootstrap 时写入的默认伙伴；nil 表示只提供角色预设。
	DefaultWaker *WakerSeed
}

// Catalog 是内置角色预设系统定义所需的最小持久化端口。
type Catalog interface {
	// Create 创建角色预设。
	Create(ctx context.Context, preset models.RolePreset) error
	// Get 按固定标识读取角色预设。
	Get(ctx context.Context, rolePresetID string) (models.RolePreset, error)
	// Update 更新角色预设的元数据和展示投影。
	Update(ctx context.Context, rolePresetID string, preset models.RolePreset) error
	// DeleteDocuments 删除角色预设落库的三份档案；系统内置预设以代码目录为权威来源。
	DeleteDocuments(ctx context.Context, rolePresetID string) error
}

// Definitions 返回内置目录定义的防御性副本。
func Definitions() []Definition {
	return append([]Definition(nil), definitions...)
}

// DefinitionByID 按稳定标识返回内置定义。
func DefinitionByID(rolePresetID string) (Definition, bool) {
	for _, definition := range definitions {
		if definition.ID == rolePresetID {
			return definition, true
		}
	}
	return Definition{}, false
}

// DefinitionByWakerID 按默认伙伴稳定标识返回关联的内置定义。
func DefinitionByWakerID(wakerID string) (Definition, bool) {
	for _, definition := range definitions {
		if definition.DefaultWaker != nil && definition.DefaultWaker.ID == wakerID {
			return definition, true
		}
	}
	return Definition{}, false
}

// DefinitionsWithDefaultWaker 返回配置了空库 bootstrap 伙伴的内置定义。
func DefinitionsWithDefaultWaker() []Definition {
	result := make([]Definition, 0, len(definitions))
	for _, definition := range definitions {
		if definition.DefaultWaker != nil {
			result = append(result, definition)
		}
	}
	return result
}

// DisplayProjection 返回面向页面的展示摘要。
func (definition Definition) DisplayProjection() profile.Projection {
	if !projectionEmpty(definition.Projection) {
		return definition.Projection
	}
	return profile.ParseDocuments(definition.Documents)
}

// Model 把系统定义转换为只读系统角色预设模型。
func (definition Definition) Model(now int64) models.RolePreset {
	projection := definition.DisplayProjection()
	return models.RolePreset{
		ID:            definition.ID,
		RoleName:      definition.RoleName,
		DefaultAvatar: definition.DefaultAvatar,
		Description:   definition.Description,
		Source:        constants.RolePresetSourceSystem,
		Expertise:     projection.Expertise,
		WorkStyle:     projection.WorkStyle,
		WorkMethods:   projection.WorkMethods,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// EnsureCatalog 幂等写入全部内置角色预设定义。
func EnsureCatalog(ctx context.Context, catalog Catalog) error {
	for _, definition := range definitions {
		if err := EnsureDefinition(ctx, catalog, definition); err != nil {
			return err
		}
	}
	return nil
}

// EnsureDefinition 幂等写入一个内置角色预设定义。
func EnsureDefinition(ctx context.Context, catalog Catalog, definition Definition) error {
	now := time.Now().UnixMilli()
	desired := definition.Model(now)
	existing, err := catalog.Get(ctx, definition.ID)
	if err == nil {
		if DefinitionCurrent(existing, definition) {
			return catalog.DeleteDocuments(ctx, definition.ID)
		}
		merged := MergeDefinition(existing, definition, now)
		if err := catalog.Update(ctx, definition.ID, merged); err != nil {
			return err
		}
		return catalog.DeleteDocuments(ctx, definition.ID)
	}
	if !hasStatusCode(err, status_error.RolePresetNotFound) {
		return err
	}
	if err := catalog.Create(ctx, desired); err == nil {
		return catalog.DeleteDocuments(ctx, definition.ID)
	} else if !hasStatusCode(err, status_error.RolePresetNameConflict) {
		return err
	}
	desired.RoleName = definition.RoleName + "（系统）"
	if err := catalog.Create(ctx, desired); err != nil {
		return err
	}
	return catalog.DeleteDocuments(ctx, definition.ID)
}

// DefinitionCurrent 判断数据库条目是否仍符合系统目录元数据。
func DefinitionCurrent(existing models.RolePreset, definition Definition) bool {
	desired := definition.Model(0)
	return (existing.RoleName == desired.RoleName ||
		existing.RoleName == desired.RoleName+"（系统）") &&
		existing.DefaultAvatar == desired.DefaultAvatar &&
		existing.Description == desired.Description &&
		existing.Source == desired.Source &&
		projectionEqual(existing, desired)
}

// MergeDefinition 刷新系统元数据并保留创建时间。
func MergeDefinition(
	existing models.RolePreset,
	definition Definition,
	now int64,
) models.RolePreset {
	desired := definition.Model(now)
	merged := desired
	if existing.RoleName == desired.RoleName+"（系统）" {
		merged.RoleName = existing.RoleName
	}
	merged.CreatedAt = existing.CreatedAt
	merged.UpdatedAt = now
	return merged
}

func projectionEqual(left models.RolePreset, right models.RolePreset) bool {
	return expertiseEqual(left.Expertise, right.Expertise) &&
		stringSlicesEqual(left.WorkStyle, right.WorkStyle) &&
		workMethodsEqual(left.WorkMethods, right.WorkMethods)
}

func expertiseEqual(left []profile.Expertise, right []profile.Expertise) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Title != right[index].Title ||
			left[index].Description != right[index].Description {
			return false
		}
	}
	return true
}

func stringSlicesEqual(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func workMethodsEqual(left []profile.WorkMethod, right []profile.WorkMethod) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Name != right[index].Name ||
			!stringSlicesEqual(left[index].Steps, right[index].Steps) {
			return false
		}
	}
	return true
}

func projectionEmpty(projection profile.Projection) bool {
	return len(projection.Expertise) == 0 &&
		len(projection.WorkStyle) == 0 &&
		len(projection.WorkMethods) == 0
}

func hasStatusCode(err error, expected status_error.StatusError) bool {
	if errors.Is(err, expected) {
		return true
	}
	var commonError statuserror.CommonError
	return errors.As(err, &commonError) && commonError.Code() == expected.Code()
}
