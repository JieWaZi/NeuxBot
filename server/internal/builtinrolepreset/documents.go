package builtinrolepreset

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// ResolveRolePresetContents 返回角色预设三份档案内容；系统内置预设直接读取目录定义。
func ResolveRolePresetContents(
	rolePresetID string,
	stored []models.RolePresetDocument,
) (map[constants.ProfileDocumentKind]string, error) {
	if definition, exists := DefinitionByID(rolePresetID); exists {
		return cloneDocuments(definition.Documents), nil
	}
	return rolePresetStoredContents(stored)
}

// ResolveWakerContents 返回伙伴三份档案内容；内置默认伙伴尚未落库时回退到目录定义。
func ResolveWakerContents(
	wakerID string,
	stored []models.WakerDocument,
) (map[constants.ProfileDocumentKind]string, error) {
	contents := make(map[constants.ProfileDocumentKind]string)
	if definition, exists := DefinitionByWakerID(wakerID); exists {
		for kind, content := range definition.Documents {
			contents[kind] = content
		}
	}
	storedEditable := false
	for _, document := range stored {
		if document.Kind == constants.ProfileDocumentKindMemory {
			continue
		}
		if !isEditableProfileKind(document.Kind) {
			return nil, status_error.ProfileDocumentKindInvalid
		}
		storedEditable = true
		contents[document.Kind] = document.Content
	}
	if _, exists := DefinitionByWakerID(wakerID); exists {
		for _, kind := range profile.EditableKinds() {
			if _, ok := contents[kind]; !ok {
				return nil, status_error.ProfileDocumentNotFound
			}
		}
		return contents, nil
	}
	if !storedEditable {
		return map[constants.ProfileDocumentKind]string{}, nil
	}
	for _, kind := range profile.EditableKinds() {
		if _, exists := contents[kind]; !exists {
			return nil, status_error.ProfileDocumentNotFound
		}
	}
	return contents, nil
}

// ResolvedRolePresetDocuments 返回角色预设三份完整档案。
func ResolvedRolePresetDocuments(
	rolePresetID string,
	stored []models.RolePresetDocument,
	updatedAt int64,
) ([]profile.Document, error) {
	contents, err := ResolveRolePresetContents(rolePresetID, stored)
	if err != nil {
		return nil, err
	}
	return documentsFromContents(contents, updatedAt, true, true), nil
}

// ResolvedWakerDocuments 返回伙伴三份可编辑档案；不含记忆文档。
func ResolvedWakerDocuments(
	wakerID string,
	stored []models.WakerDocument,
	updatedAt int64,
	includeContent bool,
	editable bool,
) ([]profile.Document, error) {
	contents, err := ResolveWakerContents(wakerID, stored)
	if err != nil {
		return nil, err
	}
	return documentsFromContents(contents, updatedAt, includeContent, editable), nil
}

func rolePresetStoredContents(
	stored []models.RolePresetDocument,
) (map[constants.ProfileDocumentKind]string, error) {
	contents := make(map[constants.ProfileDocumentKind]string, len(stored))
	for _, document := range stored {
		if !isEditableProfileKind(document.Kind) {
			return nil, status_error.ProfileDocumentKindInvalid
		}
		contents[document.Kind] = document.Content
	}
	for _, kind := range profile.EditableKinds() {
		if _, exists := contents[kind]; !exists {
			return nil, status_error.ProfileDocumentNotFound
		}
	}
	return contents, nil
}

func documentsFromContents(
	contents map[constants.ProfileDocumentKind]string,
	updatedAt int64,
	includeContent bool,
	editable bool,
) []profile.Document {
	documents := make([]profile.Document, 0, len(profile.EditableKinds()))
	for _, kind := range profile.EditableKinds() {
		content := ""
		if includeContent {
			content = contents[kind]
		}
		documents = append(documents, profile.Document{
			Kind:      kind,
			FileName:  profile.FileName(kind),
			Content:   content,
			Editable:  editable,
			UpdatedAt: updatedAt,
		})
	}
	return documents
}

func cloneDocuments(
	documents map[constants.ProfileDocumentKind]string,
) map[constants.ProfileDocumentKind]string {
	cloned := make(map[constants.ProfileDocumentKind]string, len(documents))
	for kind, content := range documents {
		cloned[kind] = content
	}
	return cloned
}

func isEditableProfileKind(kind constants.ProfileDocumentKind) bool {
	return kind == constants.ProfileDocumentKindIdentity ||
		kind == constants.ProfileDocumentKindPersona ||
		kind == constants.ProfileDocumentKindPlaybook
}
