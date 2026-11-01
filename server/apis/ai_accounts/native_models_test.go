package ai_accounts

import (
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"context"
	"errors"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	"github.com/gin-gonic/gin"
)

func codexSubscriptionQueryFixture(t *testing.T) string {
	t.Helper()
	oldAccounts, oldEnvironments, oldService := global.AIAccountControllerManager, global.RuntimeEnvironmentControllerManager, global.AIAccountService
	t.Cleanup(func() {
		global.AIAccountControllerManager, global.RuntimeEnvironmentControllerManager, global.AIAccountService = oldAccounts, oldEnvironments, oldService
	})
	id := "0123456789abcdef0123456789abcdef"
	global.AIAccountControllerManager = accountCredentialReadStub{account: models.AIAccount{
		ID: id, AuthMode: constants.AccountAuthModeSubscription, HasCredential: true,
		Endpoints: []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: constants.AIAccountOpenAIURL}},
	}}
	global.RuntimeEnvironmentControllerManager = officialThinkingEnvironment{environment: models.RuntimeEnvironment{
		RuntimeID: constants.RuntimeIDCodex, BaseRuntimeID: constants.RuntimeIDCodex, Enabled: true,
		Detection: agentapi.DetectionResult{
			State: agentapi.DetectionStateReady, DetectedDefaultModel: "saved-model",
			Models: []agentapi.ModelOption{{ID: "saved-model", ReasoningOptions: []agentapi.ReasoningOption{{ID: "high", Name: "High"}}}},
		},
	}}
	global.AIAccountService = aiaccount.New(global.AIAccountControllerManager)
	release, err := global.AIAccountService.Exclusive(id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	return id
}

func TestCodexSubscriptionQueriesReadSnapshotDuringAccountUse(t *testing.T) {
	for _, scenario := range []string{"saved-directory", "failed-refresh-retains-directory"} {
		t.Run(scenario, func(t *testing.T) {
			id := codexSubscriptionQueryFixture(t)
			if scenario == "failed-refresh-retains-directory" {
				environment, err := global.RuntimeEnvironmentControllerManager.Get(context.Background(), constants.RuntimeIDCodex)
				if err != nil {
					t.Fatal(err)
				}
				environment.Detection.Capabilities = agentapi.CapabilitySet{agentapi.CapabilityDynamicModels: true}
				environment.Detection = agentapi.PreservePreviousOptionsOnFailure(environment.Detection, agentapi.DetectionResult{
					State: agentapi.DetectionStateReady, Diagnostics: []agentapi.Diagnostic{{Code: agentapi.DiagnosticCodeACPOptionsUnavailable}},
				})
				global.RuntimeEnvironmentControllerManager = officialThinkingEnvironment{environment: environment}
			}
			for _, query := range []string{"models", "thinking", "models", "thinking"} {
				t.Run(query, func(t *testing.T) {
					ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
					requestContext, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
					defer cancel()
					ctx.Request = httptest.NewRequest("GET", "/", nil).WithContext(requestContext)
					if query == "models" {
						value, err := (&GetAIAccountNativeModels{AccountID: id, RuntimeID: constants.RuntimeIDCodex}).Output(ctx)
						if err != nil {
							t.Fatalf("saved models query waited for active account instead of reading snapshot: %v", err)
						}
						response := value.(GetAIAccountNativeModelsResponse)
						if !reflect.DeepEqual(response.Models, []string{"saved-model"}) || response.DefaultModel != "saved-model" {
							t.Fatalf("snapshot model directory was lost: %+v", response)
						}
					} else {
						value, err := (&GetAIAccountThinking{AccountID: id, RuntimeID: constants.RuntimeIDCodex, Model: "saved-model"}).Output(ctx)
						if err != nil {
							t.Fatalf("saved thinking query waited for active account instead of reading snapshot: %v", err)
						}
						response := value.(GetAIAccountThinkingResponse)
						if !response.Available || !reflect.DeepEqual(response.Options, []AccountThinkingOption{{ID: "high", Name: "High"}}) {
							t.Fatalf("snapshot reasoning menu was lost: %+v", response)
						}
					}
				})
			}
		})
	}
}

func TestCodexSubscriptionQueriesDoNotProbeMissingSnapshot(t *testing.T) {
	id := codexSubscriptionQueryFixture(t)
	for _, snapshot := range []agentapi.DetectionResult{
		{},
		{State: agentapi.DetectionStateReady},
		{
			State:       agentapi.DetectionStateReady,
			Models:      []agentapi.ModelOption{{ID: "saved-model"}},
			Diagnostics: []agentapi.Diagnostic{{Code: agentapi.DiagnosticCodeACPOptionsUnavailable}},
		},
	} {
		global.RuntimeEnvironmentControllerManager = officialThinkingEnvironment{environment: models.RuntimeEnvironment{
			RuntimeID: constants.RuntimeIDCodex, BaseRuntimeID: constants.RuntimeIDCodex, Enabled: true, Detection: snapshot,
		}}
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		requestContext, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		ctx.Request = httptest.NewRequest("GET", "/", nil).WithContext(requestContext)
		value, err := (&GetAIAccountNativeModels{AccountID: id, RuntimeID: constants.RuntimeIDCodex}).Output(ctx)
		if err != nil || len(value.(GetAIAccountNativeModelsResponse).Models) != 0 {
			cancel()
			t.Fatalf("missing directory should be empty without probing: value=%v error=%v", value, err)
		}
		value, err = (&GetAIAccountThinking{AccountID: id, RuntimeID: constants.RuntimeIDCodex, Model: "saved-model"}).Output(ctx)
		cancel()
		if err != nil || value.(GetAIAccountThinkingResponse).Available {
			t.Fatalf("missing model capabilities should remain unavailable without probing: value=%v error=%v", value, err)
		}
	}
}

func TestAPIKeyThinkingDoesNotBorrowSubscriptionSnapshot(t *testing.T) {
	id := codexSubscriptionQueryFixture(t)
	global.AIAccountControllerManager = accountCredentialReadStub{account: models.AIAccount{
		ID: id, AuthMode: constants.AccountAuthModeAPIKey, HasCredential: true, SelectedModels: []string{"saved-model"},
		Endpoints: []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: constants.AIAccountOpenAIURL}},
	}}
	global.AIAccountService = aiaccount.New(global.AIAccountControllerManager)
	release, err := global.AIAccountService.Exclusive(id)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	requestContext, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	ctx.Request = httptest.NewRequest("GET", "/", nil).WithContext(requestContext)
	_, err = (&GetAIAccountThinking{AccountID: id, RuntimeID: constants.RuntimeIDCodex, Model: "saved-model"}).Output(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("API Key capabilities must remain account-specific instead of borrowing the subscription snapshot: %v", err)
	}
}
