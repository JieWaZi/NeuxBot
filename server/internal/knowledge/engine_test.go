package knowledge

import (
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
)

func TestSourceUnavailableMapsToConflictWithoutPermissionEscalation(t *testing.T) {
	err := MapError(&core.Error{Code: core.ErrorCode("source_unavailable"), Operation: "job.sources", Detail: "accepted source was replaced"})
	if !errors.Is(err, status_error.KnowledgeConflict) {
		t.Fatalf("source lifecycle change became an unrelated permission/storage failure: %v", err)
	}
	if err = MapError(&core.Error{Code: core.ErrScopeViolation, Operation: "job", Detail: "permission revoked"}); !errors.Is(err, status_error.KnowledgeForbidden) {
		t.Fatalf("actual revoked authorization lost its boundary: %v", err)
	}
}

func TestThemeDeleteErrorsPreserveActionableReason(t *testing.T) {
	for _, tc := range []struct {
		// name 标识错误场景。
		name string
		// operation 限定 SDK 操作。
		operation string
		// detail 提供 SDK 拒绝原因。
		detail string
		// key 是客户端应收到的稳定错误标识。
		key string
	}{
		{"children", "theme.delete", "theme has children", "KnowledgeFolderHasChildren"},
		{"pages", "theme.delete", "theme contains pages", "KnowledgeFolderHasPages"},
		{"other invalid input", "theme.delete", "invalid input", "KnowledgeInvalid"},
		{"different operation", "theme.write", "theme has children", "KnowledgeInvalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := MapError(&core.Error{Code: core.ErrInvalid, Operation: tc.operation, Detail: tc.detail})
			var stable status_error.StatusError
			if !errors.As(err, &stable) || stable.Key() != tc.key {
				t.Fatalf("expected %s, got %v", tc.key, err)
			}
		})
	}
}
