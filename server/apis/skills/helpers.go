package skills

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// 技能响应
type WakerSkillResponse struct {
	// 技能包
	Skill models.SkillPackage `json:"skill"`
	// 当前版本
	CurrentVersion models.SkillVersion `json:"currentVersion"`
	// 启用状态
	Enabled bool `json:"enabled"`
	// 影响伙伴数
	AffectedWakerCount int64 `json:"affectedWakerCount"`
	// 安装伙伴
	InstalledPartners []SkillPartnerResponse `json:"installedPartners"`
}

// 技能安装伙伴
type SkillPartnerResponse struct {
	// 伙伴标识
	ID string `json:"id"`
	// 伙伴名称
	Name string `json:"name"`
	// 伙伴头像
	Avatar string `json:"avatar"`
	// 启用状态
	Enabled *bool `json:"enabled,omitempty"`
}

// resourceSkill 验证资源存在及伙伴入口的实际安装关系。
func resourceSkill(ctx context.Context, wakerID string, skillID string) (models.SkillPackage, error) {
	if wakerID == "" {
		return global.SkillControllerManager.GetPackage(ctx, skillID)
	}
	if _, err := global.WakerControllerManager.Get(ctx, wakerID); err != nil {
		return models.SkillPackage{}, err
	}
	items, err := global.SkillControllerManager.ListInstalled(ctx, wakerID, "")
	if err != nil {
		return models.SkillPackage{}, err
	}
	for _, item := range items {
		if item.SkillPackage.ID == skillID {
			return item.SkillPackage, nil
		}
	}
	return models.SkillPackage{}, status_error.SkillNotFound.WithParams(map[string]interface{}{"SkillID": skillID})
}

func wakerSkillResponses(
	ctx context.Context,
	items []models.SkillWithState,
) ([]WakerSkillResponse, error) {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.SkillPackage.ID)
	}
	if len(ids) == 0 {
		return []WakerSkillResponse{}, nil
	}
	var counts map[string]int64
	var partners map[string][]models.Waker
	err := global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error {
		var err error
		counts, err = global.SkillControllerManager.InstalledCounts(txctx, ids)
		if err != nil {
			return err
		}
		partners, err = global.SkillControllerManager.InstalledPartnerPreviews(txctx, ids)
		return err
	})
	if err != nil {
		return nil, err
	}
	responses := make([]WakerSkillResponse, 0, len(items))
	for _, item := range items {
		affectedWakerCount := counts[item.SkillPackage.ID]
		preview := []SkillPartnerResponse{}
		for _, partner := range partners[item.SkillPackage.ID] {
			preview = append(preview, SkillPartnerResponse{ID: partner.ID, Name: partner.Name, Avatar: partner.Avatar})
		}
		responses = append(responses, WakerSkillResponse{
			Skill:              item.SkillPackage,
			CurrentVersion:     item.CurrentVersion,
			Enabled:            item.Enabled,
			AffectedWakerCount: affectedWakerCount,
			InstalledPartners:  preview,
		})
	}
	return responses, nil
}

// wakerSkillDetailResponse 为详情提供完整安装伙伴，列表仍使用有限预览。
func wakerSkillDetailResponse(ctx context.Context, item models.SkillWithState) (WakerSkillResponse, error) {
	response := WakerSkillResponse{
		Skill:             item.SkillPackage,
		CurrentVersion:    item.CurrentVersion,
		Enabled:           item.Enabled,
		InstalledPartners: []SkillPartnerResponse{},
	}
	err := global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error {
		counts, err := global.SkillControllerManager.InstalledCounts(txctx, []string{item.SkillPackage.ID})
		if err != nil {
			return err
		}
		response.AffectedWakerCount = counts[item.SkillPackage.ID]
		partners, err := global.SkillControllerManager.InstalledPartners(txctx, item.SkillPackage.ID)
		if err != nil {
			return err
		}
		for _, partner := range partners {
			response.InstalledPartners = append(response.InstalledPartners, SkillPartnerResponse{
				ID: partner.ID, Name: partner.Name, Avatar: partner.Avatar, Enabled: &partner.Enabled,
			})
		}
		return nil
	})
	return response, err
}

// notifyResourceChanged 仅在对应资源写入生效后登记缓存失效。
func notifyResourceChanged(ctx context.Context, id string) {
	if global.EventHub == nil {
		return
	}
	resource := events.Resource{Kind: "skill", ID: id}
	if id == "" {
		resource = events.Resource{Kind: "global"}
	}
	events.AfterCommit(ctx, func() { _ = events.Emit(global.EventHub, "resources", "resources.changed", resource, resource) })
}
