package wakers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"github.com/shrewx/ginx/pkg/i18nx"
)

type onboardingExecutionStub struct {
	// Service 保留测试无需调用的执行端口。
	execution.Service
	// starts 记录送往执行入口的首发请求。
	starts []execution.WorkIntent
	// chatModule 保存已接纳对话的测试投影。
	chatModule *onboardingChatStub
}

func (s *onboardingExecutionStub) Start(_ context.Context, intent execution.WorkIntent) (execution.AcceptedExecution, error) {
	s.starts = append(s.starts, intent)
	if len(s.starts) == 1 {
		return execution.AcceptedExecution{}, status_error.RuntimeUnavailable
	}
	if intent.Chat.Create != nil {
		wakerID := intent.Chat.Create.WakerID
		s.chatModule.chat = models.Chat{ID: (requestidempotency.Identity{ID: intent.ClientRequestID}).ResourceID("chat", wakerID), WakerID: wakerID}
	}
	s.chatModule.chat.ActiveExecutionID = "running"
	return execution.AcceptedExecution{Chat: s.chatModule.chat}, nil
}

type onboardingChatStub struct {
	// Module 保留测试无需调用的其他 Chat 端口。
	chatservice.Module
	// chat 是已经持久的建档对话。
	chat models.Chat
	// messages 是最近已结束回合中的输入与输出。
	messages []messageview.Message
}

func (s *onboardingChatStub) LoadExecutionChat(_ context.Context, id string) (models.Chat, error) {
	if s.chat.ID == "" || s.chat.ID != id {
		return models.Chat{}, status_error.ChatNotFound
	}
	return s.chat, nil
}

func (s *onboardingChatStub) ReadExecutionMessages(_ context.Context, _, _ string) ([]messageview.Message, error) {
	return s.messages, nil
}

func TestCreateWakerOnboardingReplaysAndRecoversInitialFailure(t *testing.T) {
	i18nx.Load(&conf.I18N{Langs: []string{"zh", "en"}})
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "onboarding.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	oldDocuments := global.ProfileDocumentControllerManager
	oldWakers, oldTransactions, oldEnvironments, oldExecutions, oldChats := global.WakerControllerManager, global.TransactionControllerManager, global.RuntimeEnvironmentControllerManager, global.ExecutionService, global.ChatService
	t.Cleanup(func() {
		global.ProfileDocumentControllerManager = oldDocuments
		global.WakerControllerManager, global.TransactionControllerManager, global.RuntimeEnvironmentControllerManager, global.ExecutionService, global.ChatService = oldWakers, oldTransactions, oldEnvironments, oldExecutions, oldChats
	})
	chats := &onboardingChatStub{}
	stub := &onboardingExecutionStub{chatModule: chats}
	global.ProfileDocumentControllerManager = controllers.NewProfileDocumentController(db.DB)
	global.WakerControllerManager = controllers.NewWakerController(db.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB))
	global.RuntimeEnvironmentControllerManager = controllers.NewRuntimeEnvironmentController(db.DB)
	global.ExecutionService = stub
	global.ChatService = chats
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest("POST", "/wakers", nil)
	if err := global.RuntimeEnvironmentControllerManager.Ensure(ctx, models.RuntimeEnvironment{RuntimeID: "codex", Enabled: true, IsDefault: true}); err != nil {
		t.Fatal(err)
	}

	first := &CreateWaker{Body: CreateWakerRequest{Name: "Example", Bio: "Draft a proposal"}}
	body := &first.Body
	encoded := []byte(`{"requestId":"partner-1","name":"Example","bio":"Draft a proposal","defaultModel":{"runtimeId":"codex","accountId":"system","model":"chosen-model","reasoning":"high"}}`)
	if err := json.Unmarshal(encoded, body); err != nil {
		t.Fatal(err)
	}
	result, err := first.Output(ctx)
	if err != nil {
		t.Fatalf("initial launch failure must still return the created waker: %v", err)
	}
	firstResult := parseOnboardingResponse(t, result)
	if firstResult.WakerID == "" || firstResult.ChatID != "" || firstResult.StartError == "" {
		t.Fatalf("initial launch receipt = %+v", firstResult)
	}
	waker, err := global.WakerControllerManager.Get(ctx, firstResult.WakerID)
	if err != nil || waker.OnboardingStatus != constants.WakerOnboardingStatusPending || waker.DefaultModel == nil || waker.DefaultModel.Model != "chosen-model" {
		t.Fatalf("failed kickoff did not retain the pending partner and explicit selection: %+v, %v", waker, err)
	}
	profileResponse, err := (&GetWakerDocument{WakerID: waker.ID, Kind: constants.ProfileDocumentKindAgents}).Output(ctx)
	if document, ok := profileResponse.(profile.Document); err != nil || !ok || document.Editable || document.Content != "" {
		t.Fatalf("pending AGENTS = %+v, err = %v", profileResponse, err)
	}
	for _, kind := range []constants.ProfileDocumentKind{constants.ProfileDocumentKindAgents, constants.ProfileDocumentKindIdentity} {
		manual := &UpdateWakerDocument{WakerID: waker.ID, Kind: kind, Body: UpdateWakerDocumentRequest{Content: "unconfirmed"}}
		if _, err := manual.Output(ctx); !errors.Is(err, status_error.WakerProfileConfirmationRequired) {
			t.Fatalf("pending %s write = %v", kind, err)
		}
	}
	replay, err := first.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	replayResult := parseOnboardingResponse(t, replay)
	if replayResult.WakerID != firstResult.WakerID || replayResult.ChatID == "" || replayResult.StartError != "" {
		t.Fatalf("recovery receipt = %+v", replayResult)
	}
	if len(stub.starts) != 2 || stub.starts[0].ClientRequestID == "" || stub.starts[0].ClientRequestID != stub.starts[1].ClientRequestID {
		t.Fatalf("first launch must preserve its request identity: %+v", stub.starts)
	}
	if stub.starts[0].Options.AccountID != "system" || stub.starts[0].Options.Model != "chosen-model" ||
		stub.starts[0].Options.Reasoning != "high" || !stub.starts[0].Options.ConfigurationResolved {
		t.Fatalf("kickoff dropped the explicit model selection: %+v", stub.starts[0].Options)
	}
	chats.chat.ActiveExecutionID = ""
	chats.chat.LastEndedExecutionID = "failed-execution"
	chats.chat.LastEndedExecutionStatus = constants.ExecutionStatusFailed
	chats.messages = []messageview.Message{
		{Role: constants.MessageRoleUser, Actor: constants.WakerOnboardingActor},
		{Role: constants.MessageRoleAssistant, ErrorCode: status_error.WakerOnboardingInterviewRequired.Key(), Content: "The provider produced prose instead of opening the card"},
	}
	replay, err = first.Output(ctx)
	if err != nil || len(stub.starts) != 2 || parseOnboardingResponse(t, replay).ChatID != replayResult.ChatID {
		t.Fatalf("create replay unexpectedly retried an accepted chat: %+v, %v", replay, err)
	}
	update := &UpdateWaker{WakerID: waker.ID, Body: UpdateWakerRequest{Name: waker.Name, Bio: waker.Bio}}
	if _, err := update.Output(ctx); err != nil {
		t.Fatal(err)
	}
	waker, err = global.WakerControllerManager.Get(ctx, waker.ID)
	if err != nil || waker.DefaultModel == nil || waker.DefaultModel.Model != "chosen-model" {
		t.Fatalf("an omitted binding cleared the saved choice: %+v, %v", waker.DefaultModel, err)
	}
	update.Body.DefaultModel = &models.WakerDefaultModel{}
	if _, err := update.Output(ctx); err != nil {
		t.Fatal(err)
	}
	waker, err = global.WakerControllerManager.Get(ctx, waker.ID)
	if err != nil || waker.DefaultModel != nil {
		t.Fatalf("empty binding did not clear the saved choice: %+v, %v", waker.DefaultModel, err)
	}
	retried, err := (&StartWakerOnboarding{WakerID: waker.ID}).Output(ctx)
	if err != nil || len(stub.starts) != 3 || parseOnboardingResponse(t, retried).ChatID != replayResult.ChatID {
		t.Fatalf("explicit retry did not resume the same accepted chat: %+v, %v", retried, err)
	}
	retry := stub.starts[2]
	if retry.Chat.Create != nil || retry.Chat.ChatID != replayResult.ChatID || retry.Options.ParentExecutionID != "failed-execution" || retry.Input.SubmissionID == "" ||
		retry.Options.Model != "" || retry.Options.ConfigurationResolved {
		t.Fatalf("retry must keep the accepted chat and use the corrected defaults: %+v", retry)
	}
	first.Body.Bio = "Different input"
	if _, err := first.Output(ctx); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("changed request error = %v, want CreateRequestConflict", err)
	}
	if count, err := global.WakerControllerManager.Count(ctx, ""); err != nil || count != 1 {
		t.Fatalf("created %d wakers, err = %v", count, err)
	}
}

type onboardingTestResponse struct {
	// WakerID 是创建并持久化的伙伴标识。
	WakerID string `json:"wakerId"`
	// ChatID 是成功接纳首发后的会话标识。
	ChatID string `json:"chatId"`
	// StartError 是首发失败时的可显示原因。
	StartError string `json:"startError"`
}

func parseOnboardingResponse(t *testing.T, value any) onboardingTestResponse {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var response onboardingTestResponse
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	return response
}
