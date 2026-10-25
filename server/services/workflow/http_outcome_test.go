package workflow

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

const lostHTTPReceiptSource = `export const meta={sdk:'ally.workflow/1',title:'HTTP回执',description:'外部效果对账',actions:[{id:'post',label:'提交',description:'非幂等写入',type:'http',risk:'write',allowedOrigins:['http://workflow.example'],methods:['POST']}],phases:[{id:'write',title:'写入',detail:'写入',canExit:true}]};return await action('post',{id:'post-node',label:'提交',phase:'write',method:'POST',url:'http://workflow.example/write'});`

func TestCommittedHTTPPostRequiresReconciliationAfterLostReceipt(t *testing.T) {
	for _, mode := range []string{"disconnect", "truncated", "cancel", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "calls")
			committed := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				file, err := os.OpenFile(marker, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
				if err != nil {
					t.Error(err)
					return
				}
				_, err = file.WriteString("committed\n")
				file.Close()
				if err != nil {
					t.Error(err)
				}
				close(committed)
				if mode == "disconnect" || mode == "truncated" {
					if mode == "truncated" {
						w.Header().Set("Content-Length", "100")
						_, _ = w.Write([]byte(`{"partial":true}`))
						w.(http.Flusher).Flush()
					}
					connection, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					connection.Close()
					return
				}
				<-r.Context().Done()
			}))
			defer server.Close()
			// 仅替换 DNS/连接目标；服务端真实落盘后丢失回执，仓储与 HTTP 协议保持真实。
			dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			}
			s, _ := workflowTestService(t)
			definition := savedTestWorkflow(t, s, lostHTTPReceiptSource)
			run, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: definition.ID, RequestID: "first", Defaults: models.WorkflowExecutionDefaults{PermissionLevel: constants.PermissionLevelFullAccess}})
			if err != nil {
				t.Fatal(err)
			}
			runTicks(t, s, run.ID, 1)
			state, err := s.runs.LoadRunState(context.Background(), run.ID)
			if err != nil || len(state.Effects) != 1 {
				t.Fatalf("prepare: %+v %v", state, err)
			}
			effect := state.Effects[0]
			err = s.state.MutateEffect(context.Background(), run.ID, effect.ID, func(_ context.Context, _ *controllers.WorkflowRunState, current *models.WorkflowEffect) error {
				current.Status = constants.WorkflowStepStatusRunning
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			revision, err := s.store.GetRevision(context.Background(), definition.ID, run.RevisionID)
			if err != nil {
				t.Fatal(err)
			}
			declaration, err := workflowstate.EffectAction(revision.Manifest, effect)
			if err != nil {
				t.Fatal(err)
			}
			operationCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if mode == "timeout" {
				operationCtx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
				defer cancel()
			}
			operationDone := make(chan struct{})
			if mode == "cancel" {
				go func() {
					defer close(operationDone)
					<-committed
					if err := s.CancelRun(context.Background(), run.ID); err != nil {
						t.Error(err)
					}
					cancel()
				}()
			}
			result, unknown, failure := s.executeHTTPWithDial(operationCtx, run, declaration, workflowstate.BoundaryOptions(effect.Boundary), dial)
			if mode == "cancel" {
				<-operationDone
			}
			if failure == nil || !unknown || result != nil {
				t.Fatalf("committed action lost its unknown outcome: %#v %v %v", result, unknown, failure)
			}
			s.recordUnknownDeterministic(context.Background(), effect, result, failure)
			paused := runTicks(t, s, run.ID, 2)
			if paused.Status != constants.WorkflowRunStatusFailed {
				t.Fatalf("unreceipted action was treated as known failure: %+v", paused)
			}
			if err := s.CancelRun(context.Background(), run.ID); err != nil {
				t.Fatalf("cancel erased uncertain external outcome: %v", err)
			}
			state, err = s.runs.LoadRunState(context.Background(), run.ID)
			if err != nil || len(state.Effects) != 1 || state.Effects[0].Failure == nil {
				t.Fatalf("unknown effect receipt was lost: %+v %v", state, err)
			}
			failureDetails, ok := state.Effects[0].Failure.Data.(map[string]any)
			if !ok || (failureDetails[constants.WorkflowFailureStopRequestedField] == true) != (mode == "cancel") {
				t.Fatalf("unknown receipt lost original stop intent: %+v", state.Effects[0].Failure)
			}
			if stillPaused := runTicks(t, s, run.ID, 3); stillPaused.Status != constants.WorkflowRunStatusFailed {
				t.Fatalf("unknown external effect resumed without reconciliation: %+v", stillPaused)
			}
			content, err := os.ReadFile(marker)
			if err != nil || string(content) != "committed\n" {
				t.Fatalf("external POST repeated: %q %v", content, err)
			}
		})
	}
}

func TestHTTPFailureBeforeDispatchDoesNotRequireReconciliation(t *testing.T) {
	s, _ := workflowTestService(t)
	declaration := workflowdsl.ActionDeclaration{ID: "http", Type: "http", AllowedOrigins: []string{"http://workflow.example"}, Methods: []string{"POST"}}
	dial := func(context.Context, string, string) (net.Conn, error) { return nil, context.DeadlineExceeded }
	for _, item := range []map[string]any{{"url": "http://workflow.example/write", "method": "POST"}, {"url": "http://127.0.0.1/write", "method": "POST"}} {
		result, unknown, err := s.executeHTTPWithDial(context.Background(), models.WorkflowRun{}, declaration, item, dial)
		if err == nil || unknown || result != nil {
			t.Fatalf("undispatched request became uncertain: %#v %v %v", result, unknown, err)
		}
	}
}
