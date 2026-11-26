package controllers

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

type wakerNavigationMember struct {
	// ID 是当前伙伴的身份。
	ID string
	// NavigationGroupID 是伙伴当前所在的分组，空值表示未分组。
	NavigationGroupID string
	// NavigationPosition 是伙伴在当前分组中的显式位置，零值表示尚未设置。
	NavigationPosition int64
}

// ListNavigationGroups 查询导航分组与伙伴在各分组中保存的顺序。
func (c *wakerController) ListNavigationGroups(ctx context.Context) ([]models.WakerNavigationGroupMembers, []string, error) {
	db := controllerDB(ctx, c.db)
	var groups []models.WakerNavigationGroup
	if err := db.Order("position ASC").Find(&groups).Error; err != nil {
		return nil, nil, databaseError("list_waker_navigation_groups", "waker_navigation_groups", err)
	}
	members, err := listWakerNavigationMembers(db)
	if err != nil {
		return nil, nil, err
	}
	result := make([]models.WakerNavigationGroupMembers, len(groups))
	ungrouped := []string{}
	index := make(map[string]int, len(groups))
	for i, group := range groups {
		index[group.ID] = i
		result[i] = models.WakerNavigationGroupMembers{Name: group.Name, WakerIDs: []string{}}
	}
	for _, member := range members {
		if i, exists := index[member.NavigationGroupID]; exists {
			result[i].WakerIDs = append(result[i].WakerIDs, member.ID)
		} else {
			ungrouped = append(ungrouped, member.ID)
		}
	}
	return result, ungrouped, nil
}

func listWakerNavigationMembers(db *gorm.DB) ([]wakerNavigationMember, error) {
	var members []wakerNavigationMember
	err := db.Model(&models.Waker{}).Select("id", "navigation_group_id", "navigation_position").
		Order("navigation_group_id ASC, navigation_position = 0 ASC, navigation_position ASC, created_at ASC, id ASC").
		Find(&members).Error
	if err != nil {
		return nil, databaseError("list_waker_navigation_group_members", "waker_navigation_groups", err)
	}
	return members, nil
}

// MoveToNavigationGroup 把已读取的伙伴放到目标分组末尾，按需创建目标分组。
func (c *wakerController) MoveToNavigationGroup(ctx context.Context, waker models.Waker, name string) error {
	db := controllerDB(ctx, c.db)
	var group models.WakerNavigationGroup
	if name != "" {
		result := db.Where("name = ?", name).Limit(1).Find(&group)
		if result.Error != nil {
			return databaseError("find_waker_navigation_group", "waker_id="+waker.ID, result.Error)
		}
		if result.RowsAffected == 0 {
			var last models.WakerNavigationGroup
			if err := db.Select("position").Order("position DESC").Limit(1).Find(&last).Error; err != nil {
				return databaseError("read_waker_navigation_group_position", "waker_id="+waker.ID, err)
			}
			group = models.WakerNavigationGroup{ID: util.New(), Name: name, Position: last.Position + 1}
			if err := db.Create(&group).Error; err != nil {
				return databaseError("create_waker_navigation_group", "waker_id="+waker.ID, err)
			}
		}
	}
	if waker.NavigationGroupID == group.ID {
		return nil
	}
	members, err := listWakerNavigationMembers(db.Where("navigation_group_id = ?", group.ID))
	if err != nil {
		return err
	}
	return c.saveNavigationPlacement(ctx, db, group.ID, append(members, navigationMember(waker)))
}

// PlaceWakerInNavigation 按目标伙伴的当前分组落位，同组操作只改变顺序。
func (c *wakerController) PlaceWakerInNavigation(ctx context.Context, wakerID, targetWakerID string, placement constants.WakerNavigationPlacement) error {
	if wakerID == targetWakerID || (placement != constants.WakerNavigationPlacementBefore && placement != constants.WakerNavigationPlacementAfter) {
		return status_error.BadRequest
	}
	source, err := c.Get(ctx, wakerID)
	if err != nil {
		return err
	}
	target, err := c.Get(ctx, targetWakerID)
	if err != nil {
		return err
	}
	db := controllerDB(ctx, c.db)
	members, err := listWakerNavigationMembers(db.Where("navigation_group_id = ?", target.NavigationGroupID))
	if err != nil {
		return err
	}
	ordered := make([]wakerNavigationMember, 0, len(members)+1)
	for _, member := range members {
		if member.ID == wakerID {
			continue
		}
		if member.ID == targetWakerID && placement == constants.WakerNavigationPlacementBefore {
			ordered = append(ordered, navigationMember(source))
		}
		ordered = append(ordered, member)
		if member.ID == targetWakerID && placement == constants.WakerNavigationPlacementAfter {
			ordered = append(ordered, navigationMember(source))
		}
	}
	return c.saveNavigationPlacement(ctx, db, target.NavigationGroupID, ordered)
}

// ImportNavigationGroups 只给尚未设置过分组或顺序的伙伴导航导入旧布局。
func (c *wakerController) ImportNavigationGroups(ctx context.Context, groups []models.WakerNavigationGroupMembers, wakerOrder []string) error {
	if len(groups) == 0 && len(wakerOrder) == 0 {
		return nil
	}
	db := controllerDB(ctx, c.db)
	var first models.WakerNavigationGroup
	result := db.Select("id").Limit(1).Find(&first)
	if result.Error != nil {
		return databaseError("check_waker_navigation_groups", "waker_navigation_groups", result.Error)
	}
	if result.RowsAffected != 0 {
		return nil
	}
	var positioned wakerNavigationMember
	result = db.Model(&models.Waker{}).Select("id").Where("navigation_position > 0").Limit(1).Find(&positioned)
	if result.Error != nil {
		return databaseError("check_waker_navigation_positions", "waker_navigation_groups", result.Error)
	}
	if result.RowsAffected != 0 {
		return nil
	}
	members, err := listWakerNavigationMembers(db)
	if err != nil {
		return err
	}
	remaining := make(map[string]wakerNavigationMember, len(members))
	for _, member := range members {
		remaining[member.ID] = member
	}
	priority := make(map[string]int, len(wakerOrder))
	for i, id := range wakerOrder {
		if _, exists := priority[id]; !exists {
			priority[id] = i + 1
		}
	}
	rank := func(id string) int {
		if position, exists := priority[id]; exists {
			return position
		}
		return len(wakerOrder) + 1
	}
	names := make(map[string]struct{}, len(groups))
	for _, input := range groups {
		if _, exists := names[input.Name]; exists {
			continue
		}
		group := models.WakerNavigationGroup{ID: util.New(), Name: input.Name, Position: int64(len(names) + 1)}
		if err := db.Create(&group).Error; err != nil {
			return databaseError("import_waker_navigation_group", "group_id="+group.ID, err)
		}
		names[group.Name] = struct{}{}
		ordered := takeNavigationMembers(remaining, input.WakerIDs)
		slices.SortStableFunc(ordered, func(a, b wakerNavigationMember) int { return cmp.Compare(rank(a.ID), rank(b.ID)) })
		if _, err := writeWakerNavigationOrder(db, group.ID, ordered); err != nil {
			return err
		}
	}
	changed, err := writeWakerNavigationOrder(db, "", takeNavigationMembers(remaining, wakerOrder))
	if err != nil {
		return err
	}
	if changed || len(names) != 0 {
		c.publishNavigationGroupsChanged(ctx)
	}
	return nil
}

func takeNavigationMembers(remaining map[string]wakerNavigationMember, ids []string) []wakerNavigationMember {
	ordered := make([]wakerNavigationMember, 0, len(ids))
	for _, id := range ids {
		if member, exists := remaining[id]; exists {
			ordered = append(ordered, member)
			delete(remaining, id)
		}
	}
	return ordered
}

func navigationMember(waker models.Waker) wakerNavigationMember {
	return wakerNavigationMember{ID: waker.ID, NavigationGroupID: waker.NavigationGroupID, NavigationPosition: waker.NavigationPosition}
}

func (c *wakerController) saveNavigationPlacement(ctx context.Context, db *gorm.DB, groupID string, members []wakerNavigationMember) error {
	changed, err := writeWakerNavigationOrder(db, groupID, members)
	if err == nil && changed {
		c.publishNavigationGroupsChanged(ctx)
	}
	return err
}

func writeWakerNavigationOrder(db *gorm.DB, groupID string, members []wakerNavigationMember) (bool, error) {
	const batchSize = 150
	changed := false
	for start := 0; start < len(members); start += batchSize {
		end := min(start+batchSize, len(members))
		ids := make([]string, 0, end-start)
		args := make([]any, 0, 2*(end-start))
		var positions strings.Builder
		positions.WriteString("CASE id")
		for offset, member := range members[start:end] {
			position := int64(start + offset + 1)
			if member.NavigationGroupID == groupID && member.NavigationPosition == position {
				continue
			}
			ids = append(ids, member.ID)
			positions.WriteString(" WHEN ? THEN ?")
			args = append(args, member.ID, position)
		}
		if len(ids) == 0 {
			continue
		}
		positions.WriteString(" END")
		if err := db.Model(&models.Waker{}).Where("id IN ?", ids).UpdateColumns(map[string]any{
			"navigation_group_id": groupID,
			"navigation_position": gorm.Expr(positions.String(), args...),
		}).Error; err != nil {
			return false, databaseError("write_waker_navigation_order", "group_id="+groupID, err)
		}
		changed = true
	}
	return changed, nil
}

func (c *wakerController) publishNavigationGroupsChanged(ctx context.Context) {
	publishCommittedChange(ctx, c.db, "entities", eventsResource(constants.WakerNavigationGroupsEventKind, constants.WakerNavigationGroupsEventID))
}
