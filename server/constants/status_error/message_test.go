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
