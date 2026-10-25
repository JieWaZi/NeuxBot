package workflows

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

func TestHumanFormValidatesTypedAnswersAndNeverSubmitsDefaults(t *testing.T) {
	s, _ := workflowTestService(t)
	source := `export const meta={sdk:'ally.workflow/1',title:'会议资料表单',phases:[{id:'inputs',title:'收集资料',detail:'用户提交后继续',canExit:true}]};
const answer=await askUser('填写会议资料', {id:'meeting_input',label:'会议资料',phase:'inputs',schema:{type:'object',properties:{name:{type:'string','x-ui-control':'input'},format:{type:'string',enum:['brief','full'],'x-ui-control':'radio','x-ui-labels':['简洁','完整']},topics:{type:'array',items:{type:'string',enum:['agenda','materials']},uniqueItems:true,'x-ui-control':'checkboxes'},confirmed:{type:'boolean','x-ui-control':'switch'},notes:{type:'string','x-ui-control':'textarea'}},required:['name','format','topics','confirmed','notes'],additionalProperties:false},defaultValue:{name:'周会',format:'brief',topics:['agenda'],confirmed:false,notes:''},allowSkip:true});
return {approved:answer.source==='user' && answer.value.confirmed===true, source:answer.source, value:answer.value};`
	definition := savedTestWorkflow(t, s, source)
	run, err := s.StartRun(context.Background(), workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "run"})
	if err != nil {
		t.Fatal(err)
	}
	if actual := runTicks(t, s, run.ID, 3); actual.Status != constants.WorkflowRunStatusWaiting {
		t.Fatal("defaults submitted without user action")
	}
	state, err := s.Runs.LoadRunState(context.Background(), run.ID)
	if err != nil || len(state.Humans) != 1 || state.Humans[0].Value != nil {
		t.Fatalf("form is not waiting: %+v %v", state.Humans, err)
	}
	answer := map[string]any{"name": "周会", "format": "full", "topics": []any{"agenda", "materials"}, "confirmed": "true", "notes": "第一行\n第二行"}
	command := AnswerHumanCommand{RunID: run.ID, HumanID: state.Humans[0].ID, RequestID: "invalid", Value: answer}
	if err := answerHuman(context.Background(), workflowAPIDependencies(s), command); workflowstate.ErrorKey(err) != status_error.WorkflowSchemaInvalid.Key() {
		t.Fatalf("invalid boolean accepted: %v", err)
	}
	answer["confirmed"] = true
	command.RequestID = "valid"
	if err := answerHuman(context.Background(), workflowAPIDependencies(s), command); err != nil {
		t.Fatal(err)
	}
	result := runTicks(t, s, run.ID, 4)
	value, ok := result.Result.(map[string]any)
	if !ok || result.Status != constants.WorkflowRunStatusSucceeded || value["approved"] != true || value["source"] != "user" {
		t.Fatalf("typed answer did not resume: %+v", result)
	}
	fields, ok := value["value"].(map[string]any)
	if !ok || fields["confirmed"] != true || fields["notes"] != answer["notes"] {
		t.Fatalf("typed form values changed: %#v", value)
	}
}
