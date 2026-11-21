package workflows

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
)

func TestValidateWorkflowSourceReportsUnsupportedWorkerConnectorPolicy(t *testing.T) {
	fixture, store := workflowTestService(t)
	definition := savedTestWorkflow(t, fixture, humanSource)
	previousStore := global.WorkflowControllerManager
	global.WorkflowControllerManager = store
	t.Cleanup(func() { global.WorkflowControllerManager = previousStore })
	ginContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	source := `export const meta={sdk:'bot.workflow',title:'复核',description:'复核',phases:[{id:'check',title:'检查',detail:'检查'}]};
return await worker('复核',{id:'review',phase:'check',label:'复核',resolve:{kind:'waker',wakerId:'one'},toolPolicy:{connectors:CONNECTOR_POLICY}});`
	operator := ValidateWorkflowSource{ID: definition.ID, Body: ValidateWorkflowSourceRequest{
		SDK: constants.WorkflowSDKIdentifier, Source: strings.Replace(source, "CONNECTOR_POLICY", `{mode:'allowlist',connectorIds:[]}`, 1),
	}}
	value, err := operator.Output(ginContext)
	if err != nil {
		t.Fatal(err)
	}
	invalid := value.(WorkflowValidationResponse)
	if invalid.Valid || invalid.Manifest != nil || len(invalid.Diagnostics) != 1 {
		t.Fatalf("unsupported policy unexpectedly validated: %+v", invalid)
	}
	diagnostic := invalid.Diagnostics[0]
	if diagnostic.Code != "WORKER_TOOL_POLICY_UNSUPPORTED" || diagnostic.Field != "toolPolicy.connectors" || diagnostic.NodeID != "review" || diagnostic.Range.Start.Line != 2 {
		t.Fatalf("policy diagnostic did not identify the node and source field: %+v", diagnostic)
	}
	operator.Body.Source = strings.Replace(source, "CONNECTOR_POLICY", `{mode:'inherit'}`, 1)
	value, err = operator.Output(ginContext)
	if err != nil {
		t.Fatal(err)
	}
	valid := value.(WorkflowValidationResponse)
	if !valid.Valid || valid.Manifest == nil || len(valid.Diagnostics) != 0 {
		t.Fatalf("inherit did not validate: %+v", valid)
	}
}
