package execution

import (
	"context"
	"encoding/json"
	"time"

	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"testing"
	"testing/synctest"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversationfixture"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
)

func TestValidateElicitationContentUsesFrozenSchema(t *testing.T) {
	schema := json.RawMessage(`{
		"type":"object",
		"properties":{
			"mode":{"type":"string","enum":["safe","fast"]},
			"retries":{"type":"integer"}
		},
		"required":["mode"],
		"additionalProperties":false
	}`)
	requested := eventlog.ElicitationRequestedData{Mode: "form", RequestedSchema: schema}
	for name, content := range map[string]map[string]any{
		"missing required": {},
		"wrong type":       {"mode": "safe", "retries": "2"},
		"outside enum":     {"mode": "unsafe"},
		"extra field":      {"mode": "safe", "other": true},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateElicitationContent(requested, content); !errors.Is(err, status_error.BadRequest) {
				t.Fatalf("validation error = %v, want BadRequest", err)
			}
		})
	}
	if err := validateElicitationContent(requested, map[string]any{"mode": "fast", "retries": float64(2)}); err != nil {
		t.Fatalf("valid content rejected: %v", err)
	}
}

func TestNativeQuestionsSharePendingFactsAndResolveExactlyOnce(t *testing.T) {
	for _, action := range []string{"answer", "decline", "cancel"} {
		t.Run(action, func(t *testing.T) {
			store := conversationfixture.New(t, t.TempDir())
			chatID := "51515151515151515151515151515151"
			if _, err := conversationfixture.CommitFacts(t.Context(), store, chatID, []eventlog.AppendInput{{Type: constants.ChatEventTypeCreated}}); err != nil {
				t.Fatal(err)
			}
			journal := &consentTestJournal{interactionTestJournal: &interactionTestJournal{store: store}, updates: make(chan []models.PendingElicitation, 4)}
			coordinator := newElicitationCoordinator(journal)
			if len(coordinator.pendingProjection(chatID)) > 0 {
				t.Fatal("undelivered request appeared in the pending projection")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan agentapi.ElicitationDecision, 1)
			go func() {
				decision, err := coordinator.handler(chatID, "execution").Request(ctx, agentapi.ElicitationRequest{InvocationID: "invocation", Mode: "form", Message: "选择交付方式", RequestedSchema: map[string]any{"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}}, "required": []string{"answer"}}})
				if err != nil {
					t.Errorf("request failed: %v", err)
				}
				done <- decision
			}()
			pending := <-journal.updates
			if len(pending) != 1 || !(len(coordinator.pendingProjection(chatID)) > 0) {
				t.Fatalf("durable request missing: %+v", pending)
			}
			id := pending[0].ElicitationID
			var err error
			wantResult := constants.ElicitationResultAccepted
			switch action {
			case "answer":
				err = coordinator.resolveAccepted(context.Background(), chatID, id, map[string]any{"answer": "file"})
			case "decline":
				wantResult = constants.ElicitationResultDeclined
				err = coordinator.resolveDeclined(context.Background(), chatID, id)
			case "cancel":
				wantResult = constants.ElicitationResultInvalidated
				cancel()
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case decision := <-done:
				if decision.Accepted != (action == "answer") || decision.Declined != (action == "decline") || decision.Cancelled != (action == "cancel") {
					t.Fatalf("wrong native decision: %+v", decision)
				}
			case <-time.After(time.Second):
				t.Fatal("native wait was not released")
			}
			if len(coordinator.pendingProjection(chatID)) > 0 {
				t.Fatal("resolved request remained in the pending projection")
			}
			if err := coordinator.resolveAccepted(context.Background(), chatID, id, map[string]any{"answer": "late"}); !errors.Is(err, status_error.ElicitationAlreadyResolved) {
				t.Fatalf("late response accepted: %v", err)
			}
			records, err := conversationfixture.ReadFacts(t.Context(), store, chatID)
			if err != nil {
				t.Fatal(err)
			}
			if len(records) != 3 {
				t.Fatalf("resolution was not unique: %+v", records)
			}
			var resolved eventlog.ElicitationResolvedData
			if err := json.Unmarshal(records[2].Data, &resolved); err != nil || resolved.Result != wantResult {
				t.Fatalf("wrong durable resolution: %+v %v", resolved, err)
			}
		})
	}
}

// interactionDeadlineJournal 保留真实JSONL，仅控制已接纳决定发布跨过截止点的时序。
type interactionDeadlineJournal struct {
	// Journal 保留本用例未使用的日志端口。
	chatservice.Journal
	// store 保存请求与处理的权威事实。
	store *conversation.Store
	// published 把请求已交付的时刻通知测试。
	published chan eventlog.Record
	// afterResolution 在处理事实持久化后、调用方发布完成前控制交错。
	afterResolution func()
	// duringPublication holds a committed decision before delivery to the native waiter.
	duringPublication func()
}

func (j *interactionDeadlineJournal) append(chatID string, input eventlog.AppendInput) (eventlog.Record, error) {
	records, err := conversationfixture.CommitFacts(context.Background(), j.store, chatID, []eventlog.AppendInput{input})
	if err != nil {
		return eventlog.Record{}, err
	}
	if (input.Type == constants.ChatEventTypeElicitationResolved || input.Type == constants.ChatEventTypeApprovalResolved) && j.afterResolution != nil {
		j.afterResolution()
	}
	return records[0], nil
}
func (j *interactionDeadlineJournal) AppendElicitationFact(chatID string, input eventlog.AppendInput) (eventlog.Record, error) {
	return j.append(chatID, input)
}
func (j *interactionDeadlineJournal) AppendApprovalFact(chatID string, input eventlog.AppendInput) (eventlog.Record, error) {
	return j.append(chatID, input)
}
func (j *interactionDeadlineJournal) PublishElicitationFact(_ context.Context, r eventlog.Record, _ []models.PendingElicitation) {
	if r.Type == constants.ChatEventTypeElicitationResolved && j.duringPublication != nil {
		j.duringPublication()
	}
	j.published <- r
}
func (j *interactionDeadlineJournal) PublishApprovalFact(_ context.Context, r eventlog.Record, _ []models.PendingApproval) {
	if r.Type == constants.ChatEventTypeApprovalResolved && j.duringPublication != nil {
		j.duringPublication()
	}
	j.published <- r
}

func TestNativeInteractionAdmissionOwnsDeadlineRace(t *testing.T) {
	for _, approval := range []bool{false, true} {
		for _, late := range []bool{false, true} {
			name := "input"
			if approval {
				name = "approval"
			}
			if late {
				name += "/after_deadline"
			} else {
				name += "/accepted_before_deadline"
			}
			t.Run(name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					store := conversationfixture.New(t, t.TempDir())
					chatID := "52525252525252525252525252525252"
					if _, err := conversationfixture.CommitFacts(t.Context(), store, chatID, []eventlog.AppendInput{{Type: constants.ChatEventTypeCreated}}); err != nil {
						t.Fatal(err)
					}
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
					defer cancel()
					journal := &interactionDeadlineJournal{store: store, published: make(chan eventlog.Record, 4), afterResolution: func() { <-ctx.Done() }}
					inputs := newElicitationCoordinator(journal)
					approvals := newApprovalCoordinator(journal)
					done := make(chan bool, 1)
					go func() {
						if approval {
							decision, err := approvals.handler(chatID, "execution").Request(ctx, agentapi.ApprovalRequest{Title: "批准读取", Options: []agentapi.ApprovalOption{{ID: "allow", Kind: "allow_once"}}})
							if err != nil {
								t.Errorf("approval: %v", err)
							}
							done <- !decision.Cancelled
						} else {
							decision, err := inputs.handler(chatID, "execution").Request(ctx, agentapi.ElicitationRequest{Mode: "form", Message: "选择交付方式", RequestedSchema: map[string]any{"type": "object"}})
							if err != nil {
								t.Errorf("input: %v", err)
							}
							done <- decision.Accepted
						}
					}()
					requested := <-journal.published
					var id string
					if approval {
						var data eventlog.ApprovalRequestedData
						_ = json.Unmarshal(requested.Data, &data)
						id = data.ApprovalID
					} else {
						var data eventlog.ElicitationRequestedData
						_ = json.Unmarshal(requested.Data, &data)
						id = data.ElicitationID
					}
					if late {
						// 持有既有resolution锁延迟自动作废，验证到期本身已禁止新的用户接纳。
						if approval {
							pending, err := approvals.lockPendingResolution(chatID, id)
							if err != nil {
								t.Fatal(err)
							}
							<-ctx.Done()
							_, _, err = approvals.beginResolution(chatID, id, "allow", true)
							if !errors.Is(err, status_error.ApprovalAlreadyResolved) {
								t.Errorf("expired approval admitted before invalidation: %v", err)
							}
							pending.resolutionMutex.Unlock()
						} else {
							pending, err := inputs.lockPendingResolution(chatID, id)
							if err != nil {
								t.Fatal(err)
							}
							<-ctx.Done()
							_, err = inputs.beginResolution(chatID, id, true)
							if !errors.Is(err, status_error.ElicitationAlreadyResolved) {
								t.Errorf("expired input admitted before invalidation: %v", err)
							}
							pending.resolutionMutex.Unlock()
						}
					} else {
						// 用户在截止前获得处理权；真实accepted日志写入后才推进到期限。
						var err error
						if approval {
							err = approvals.resolveSelected(context.Background(), chatID, id, "allow")
						} else {
							err = inputs.resolveAccepted(context.Background(), chatID, id, map[string]any{})
						}
						if err != nil {
							t.Fatal(err)
						}
					}
					if accepted := <-done; accepted == late {
						t.Fatalf("wrong resolution winner: accepted=%v late=%v", accepted, late)
					}
					if len(inputs.pendingProjection(chatID)) != 0 || len(approvals.pendingProjection(chatID)) != 0 {
						t.Fatal("finished request remained pending")
					}
					records, err := conversationfixture.ReadFacts(t.Context(), store, chatID)
					if err != nil {
						t.Fatal(err)
					}
					if len(records) != 3 {
						t.Fatalf("resolution not unique: %d", len(records))
					}
					var resolved struct {
						// Result 是持久化的唯一处理结果。
						Result string `json:"result"`
					}
					_ = json.Unmarshal(records[2].Data, &resolved)
					expected := "accepted"
					if approval {
						expected = "selected"
					}
					if late {
						expected = "invalidated"
					}
					if resolved.Result != expected {
						t.Fatalf("durable result=%s want=%s", resolved.Result, expected)
					}
				})
			})
		}
	}
}

func TestNativeInteractionKeepsCommittedDecisionDuringPublication(t *testing.T) {
	for _, approval := range []bool{true, false} {
		t.Run(map[bool]string{true: "approval", false: "input"}[approval], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				store := conversationfixture.New(t, t.TempDir())
				chatID := "52525252525252525252525252525252"
				if _, err := conversationfixture.CommitFacts(t.Context(), store, chatID, []eventlog.AppendInput{{Type: constants.ChatEventTypeCreated}}); err != nil {
					t.Fatal(err)
				}
				publishing, release := make(chan struct{}), make(chan struct{})
				journal := &interactionDeadlineJournal{store: store, published: make(chan eventlog.Record, 2), duringPublication: func() {
					close(publishing)
					<-release
				}}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				requestedResult := make(chan bool, 1)
				resolutionError := make(chan error, 1)
				if approval {
					coordinator := newApprovalCoordinator(journal)
					go func() {
						decision, err := coordinator.handler(chatID, "execution").Request(ctx, agentapi.ApprovalRequest{
							Options: []agentapi.ApprovalOption{{ID: "allow"}},
						})
						requestedResult <- err == nil && decision.OptionID == "allow"
					}()
					var requested eventlog.ApprovalRequestedData
					_ = json.Unmarshal((<-journal.published).Data, &requested)
					go func() {
						resolutionError <- coordinator.resolveSelected(context.Background(), chatID, requested.ApprovalID, "allow")
					}()
				} else {
					coordinator := newElicitationCoordinator(journal)
					go func() {
						decision, err := coordinator.handler(chatID, "execution").Request(ctx, agentapi.ElicitationRequest{
							Mode: "form", RequestedSchema: map[string]any{"type": "object"},
						})
						requestedResult <- err == nil && decision.Accepted
					}()
					var requested eventlog.ElicitationRequestedData
					_ = json.Unmarshal((<-journal.published).Data, &requested)
					go func() {
						resolutionError <- coordinator.resolveAccepted(context.Background(), chatID, requested.ElicitationID, nil)
					}()
				}
				<-publishing
				cancel()
				synctest.Wait()
				select {
				case accepted := <-requestedResult:
					close(release)
					t.Fatalf("request returned before publishing the committed decision: accepted=%v", accepted)
				default:
				}
				close(release)
				if err := <-resolutionError; err != nil {
					t.Fatal(err)
				}
				if accepted := <-requestedResult; !accepted {
					t.Fatal("cancel replaced a committed user decision")
				}
			})
		})
	}
}
