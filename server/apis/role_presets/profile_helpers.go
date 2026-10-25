package role_presets

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func resolveRolePresetContents(
	existing map[constants.ProfileDocumentKind]string,
	request RolePresetWriteRequest,
) (map[constants.ProfileDocumentKind]string, error) {
	contents := map[constants.ProfileDocumentKind]string{
		constants.ProfileDocumentKindIdentity: existing[constants.ProfileDocumentKindIdentity],
		constants.ProfileDocumentKindPersona:  existing[constants.ProfileDocumentKindPersona],
		constants.ProfileDocumentKindPlaybook: existing[constants.ProfileDocumentKindPlaybook],
	}
	switch request.InputMode {
	case constants.RolePresetInputModeManual:
		contents = profile.PatchManual(contents, request.Manual)
	case constants.RolePresetInputModeMarkdown:
		if request.Documents.Identity != nil {
			contents[constants.ProfileDocumentKindIdentity] = *request.Documents.Identity
		}
		if request.Documents.Persona != nil {
			contents[constants.ProfileDocumentKindPersona] = *request.Documents.Persona
		}
		if request.Documents.Playbook != nil {
			contents[constants.ProfileDocumentKindPlaybook] = *request.Documents.Playbook
		}
	default:
		return nil, status_error.BadRequest
	}
	for _, kind := range profile.EditableKinds() {
		if err := profile.ValidateContent(contents[kind]); err != nil {
			return nil, err
		}
	}
	return contents, nil
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

func isEditableProfileKind(kind constants.ProfileDocumentKind) bool {
	return kind == constants.ProfileDocumentKindIdentity ||
		kind == constants.ProfileDocumentKindPersona ||
		kind == constants.ProfileDocumentKindPlaybook
}

func rolePresetProfileDocument(document models.RolePresetDocument) profile.Document {
	return profile.Document{
		Kind:      document.Kind,
		FileName:  profile.FileName(document.Kind),
		Content:   document.Content,
		Editable:  true,
		UpdatedAt: document.UpdatedAt,
	}
}
