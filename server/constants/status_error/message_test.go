package status_error

import (
	"errors"
	"fmt"
	"github.com/shrewx/ginx/pkg/statuserror"
	"testing"
)

func TestPublicErrorKeepsFirstApplicationFailure(t *testing.T) {
	original := WorkspacePathInvalid.WithParams(map[string]interface{}{"field": "workspace"})
	err := errors.Join(fmt.Errorf("private wrapper: %w", original), RuntimeExecutionFailed.WithMessage("native secondary reason"))
	public := PublicError(err)
	var detail statuserror.CommonError
	if !errors.As(public, &detail) || detail.Key() != "WorkspacePathInvalid" {
		t.Fatalf("public failure = %v", public)
	}
}

func TestPublicParamsUsesTemplateFieldsAndDropsPrivatePayload(t *testing.T) {
	params := PublicParams(RuntimeWorkspaceScopeUnsupported.WithParams(map[string]interface{}{"MaxDirectories": 2, "Credential": "private"}))
	if len(params) != 1 || params["MaxDirectories"] != 2 {
		t.Fatalf("public parameters = %#v", params)
	}
	params = PublicParams(RuntimeExecutionFailed.WithParams(map[string]interface{}{"ModelUnavailable": true, "Detail": "private detail"}))
	if len(params) != 1 || params["ModelUnavailable"] != true {
		t.Fatalf("conditional parameters = %#v", params)
	}
}

// TestAutomaticTaskConflictCodesRemainStable 验证公开自动任务错误的数值协议不受其他错误移除影响。
func TestAutomaticTaskConflictCodesRemainStable(t *testing.T) {
	for code, expected := range map[StatusError]int{
		AutomaticTaskBusy:             40900000033,
		AutomaticTaskDisabled:         40900000034,
		AutomaticTaskRequestConflict:  40900000035,
		AutomaticTaskRevisionConflict: 40900000036,
		AutomaticTaskAdmissionRevoked: 40900000037,
	} {
		if int(code) != expected {
			t.Errorf("%s code=%d want=%d", code.Key(), code, expected)
		}
	}
}
