package wakers

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func validProfileDocumentKind(kind constants.ProfileDocumentKind) bool {
	return isEditableProfileKind(kind)
}

func isEditableProfileKind(kind constants.ProfileDocumentKind) bool {
	return kind == constants.ProfileDocumentKindIdentity ||
		kind == constants.ProfileDocumentKindPersona ||
		kind == constants.ProfileDocumentKindPlaybook
}

func rolePresetDocumentContents(
	documents []models.RolePresetDocument,
) (map[constants.ProfileDocumentKind]string, error) {
	contents := make(map[constants.ProfileDocumentKind]string, len(documents))
	for _, document := range documents {
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

func wakerDocumentContents(
	documents []models.WakerDocument,
) map[constants.ProfileDocumentKind]string {
	contents := make(map[constants.ProfileDocumentKind]string, len(documents))
	for _, document := range documents {
		contents[document.Kind] = document.Content
	}
	return contents
}

func wakerProfileDocument(
	document models.WakerDocument,
	includeContent bool,
) profile.Document {
	content := ""
	if includeContent {
		content = document.Content
	}
	return profile.Document{
		Kind:      document.Kind,
		FileName:  profile.FileName(document.Kind),
		Content:   content,
		Editable:  true,
		UpdatedAt: document.UpdatedAt,
	}
}
