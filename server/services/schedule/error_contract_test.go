package schedule

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"testing"
)

func TestAutomaticRunKeepsAuthoritativeFailurePresentation(t *testing.T) {
	execution := &models.Execution{ID: "execution", Status: constants.ExecutionStatusFailed, ErrorCode: "RuntimeWorkspaceScopeUnsupported", ErrorParams: map[string]interface{}{"MaxDirectories": 2}, ErrorMessageSource: constants.ErrorMessageSourceLocalized}
	states := map[string]chat.InitialExecutionState{"chat": {InitialExecution: execution}}
	run := models.AutomaticRun{ChatID: "chat", ExecutionID: "execution", Admission: constants.AutomaticRunAdmissionAccepted}
	view, err := New(Config{}).ReadRunWithSnapshots(t.Context(), run, states)
	if err != nil {
		t.Fatal(err)
	}
	if view.State != constants.AutomaticRunStateFailed || view.ErrorCode != "RuntimeWorkspaceScopeUnsupported" || view.ErrorParams["MaxDirectories"] != 2 || view.ErrorMessageSource != "localized" {
		t.Fatalf("automatic failure = %+v", view)
	}
	execution.ErrorCode = "RuntimeQuotaExhausted"
	execution.ErrorParams = nil
	execution.ErrorMessage = "Native quota exhausted"
	execution.ErrorMessageSource = constants.ErrorMessageSourcePublic
	view, err = New(Config{}).ReadRunWithSnapshots(t.Context(), run, states)
	if err != nil || view.ErrorMessage != "Native quota exhausted" || view.ErrorMessageSource != "public" {
		t.Fatalf("automatic public reason = %+v error=%v", view, err)
	}
}
