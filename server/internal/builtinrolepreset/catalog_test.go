package builtinrolepreset

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

type catalogFake struct {
	// presets 保存测试中的角色目录。
	presets map[string]models.RolePreset
	// deletedDocuments 记录已清理持久化档案的角色标识。
	deletedDocuments []string
}

func (fake *catalogFake) Create(_ context.Context, preset models.RolePreset) error {
	if _, exists := fake.presets[preset.ID]; exists {
		return status_error.RolePresetNameConflict
	}
	for _, existing := range fake.presets {
		if existing.RoleName == preset.RoleName {
			return status_error.RolePresetNameConflict
		}
	}
	fake.presets[preset.ID] = preset
	return nil
}

func (fake *catalogFake) Get(_ context.Context, rolePresetID string) (models.RolePreset, error) {
	preset, exists := fake.presets[rolePresetID]
	if !exists {
		return models.RolePreset{}, status_error.RolePresetNotFound
	}
	return preset, nil
}

func (fake *catalogFake) Update(
	_ context.Context,
	rolePresetID string,
	preset models.RolePreset,
) error {
	fake.presets[rolePresetID] = preset
	return nil
}

func (fake *catalogFake) DeleteDocuments(_ context.Context, rolePresetID string) error {
	fake.deletedDocuments = append(fake.deletedDocuments, rolePresetID)
	return nil
}

func TestEnsureCatalogSeedsPersonalAssistantPreset(t *testing.T) {
	catalog := &catalogFake{
		presets: make(map[string]models.RolePreset),
	}
	if err := EnsureCatalog(context.Background(), catalog); err != nil {
		t.Fatalf("EnsureCatalog() error = %v", err)
	}
	preset, exists := catalog.presets[PersonalAssistantRolePresetID]
	if !exists {
		t.Fatal("missing personal assistant role preset")
	}
	if preset.Source != constants.RolePresetSourceSystem {
		t.Fatalf("source = %q, want system", preset.Source)
	}
	if preset.DefaultAvatar != constants.PersonalAssistantAvatar {
		t.Fatalf("default avatar = %q", preset.DefaultAvatar)
	}
	if preset.RoleName != "私人助理" {
		t.Fatalf("role name = %q", preset.RoleName)
	}
	if len(preset.Expertise) != 5 || len(preset.WorkStyle) != 5 || len(preset.WorkMethods) != 4 {
		t.Fatalf("unexpected projection: %+v", preset)
	}
	if len(catalog.deletedDocuments) != len(Definitions()) ||
		catalog.deletedDocuments[0] != PersonalAssistantRolePresetID {
		t.Fatalf("EnsureCatalog() must not persist documents: %+v", catalog.deletedDocuments)
	}
	if err := EnsureCatalog(context.Background(), catalog); err != nil {
		t.Fatalf("EnsureCatalog() must be idempotent: %v", err)
	}
}
