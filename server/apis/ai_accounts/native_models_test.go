package ai_accounts

import (
	"context"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	"github.com/JieWaZi/NeuxBot/server/services/runtimeenvironment"
	"github.com/gin-gonic/gin"
)

type codexAccountCatalogStub struct {
	// Service retains unrelated runtime operations.
	runtimeenvironment.Service
	// read checks the selected account and returns its native discovery result.
	read func(string, string, bool) runtimeenvironment.AccountModelCatalog
}

func (s codexAccountCatalogStub) ReadModelCatalog(_ context.Context, runtimeID, accountID string, refresh bool) (runtimeenvironment.AccountModelCatalog, error) {
	return s.read(runtimeID, accountID, refresh), nil
}

func codexAccountQueryFixture(t *testing.T, mode constants.AccountAuthMode, state string) string {
	t.Helper()
	oldAccounts, oldEnvironments := global.AIAccountControllerManager, global.RuntimeEnvironmentControllerManager
	oldService, oldCatalog := global.AIAccountService, global.RuntimeEnvironmentService
	t.Cleanup(func() {
		global.AIAccountControllerManager, global.RuntimeEnvironmentControllerManager = oldAccounts, oldEnvironments
		global.AIAccountService, global.RuntimeEnvironmentService = oldService, oldCatalog
	})
	id := "0123456789abcdef0123456789abcdef"
	global.AIAccountControllerManager = accountCredentialReadStub{account: models.AIAccount{
		ID: id, AuthMode: mode, HasCredential: true, SelectedModels: []string{"account-model"},
		Endpoints: []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: constants.AIAccountOpenAIURL}},
	}}
	global.AIAccountService = aiaccount.New(global.AIAccountControllerManager)
	global.RuntimeEnvironmentControllerManager = officialThinkingEnvironment{environment: models.RuntimeEnvironment{
		RuntimeID: constants.RuntimeIDCodex, BaseRuntimeID: constants.RuntimeIDCodex, Enabled: true,
		Detection: agentapi.DetectionResult{
			State: agentapi.DetectionStateReady, DetectedDefaultModel: "other-account-model",
			Models: []agentapi.ModelOption{{ID: "other-account-model"}},
		},
	}}
	global.RuntimeEnvironmentService = codexAccountCatalogStub{read: func(runtimeID, accountID string, refresh bool) runtimeenvironment.AccountModelCatalog {
		t.Helper()
		if runtimeID != constants.RuntimeIDCodex || accountID != id || refresh {
			t.Fatal("query changed the selected native catalog identity")
		}
		return runtimeenvironment.AccountModelCatalog{
			AccountID: id,
			ModelCatalogSnapshot: agent.ModelCatalogSnapshot{
				State: state,
				AgentModelCatalog: agent.AgentModelCatalog{
					DefaultID: "account-model",
					Models: []agent.AgentChoice{{ID: "account-model", Config: &agent.AgentModelConfig{
						Options: []agent.AgentSessionOption{{ID: "reasoning_effort", Choices: []agent.AgentChoice{{ID: "high", Name: "High"}}}},
					}}},
				},
			},
		}
	}}
	return id
}

func TestCodexAccountQueriesOnlyReadSelectedNativeCatalog(t *testing.T) {
	for _, state := range []string{agent.CatalogFresh, agent.CatalogStale, agent.CatalogUnavailable, agent.CatalogSignedOut} {
		t.Run(state, func(t *testing.T) {
			id := codexAccountQueryFixture(t, constants.AccountAuthModeSubscription, state)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("GET", "/", nil)
			value, err := (&GetAIAccountNativeModels{AccountID: id, RuntimeID: constants.RuntimeIDCodex}).Output(ctx)
			if err != nil {
				t.Fatal(err)
			}
			models := value.(GetAIAccountNativeModelsResponse)
			value, err = (&GetAIAccountThinking{AccountID: id, RuntimeID: constants.RuntimeIDCodex, Model: "account-model"}).Output(ctx)
			if err != nil {
				t.Fatal(err)
			}
			thinking := value.(GetAIAccountThinkingResponse)
			if state == agent.CatalogFresh || state == agent.CatalogStale {
				if !reflect.DeepEqual(models.Models, []string{"account-model"}) || models.DefaultModel != "account-model" {
					t.Fatalf("selected catalog lost its identity: %+v", models)
				}
				if !thinking.Available || !reflect.DeepEqual(thinking.Options, []AccountThinkingOption{{ID: "high", Name: "High"}}) {
					t.Fatalf("selected model lost its reasoning catalog: %+v", thinking)
				}
			} else if len(models.Models) != 0 || models.DefaultModel != "" || thinking.Available {
				t.Fatalf("unavailable account used another catalog: %+v %+v", models, thinking)
			}
		})
	}
}

func TestCodexAPIKeyThinkingUsesSelectedNativeCatalog(t *testing.T) {
	id := codexAccountQueryFixture(t, constants.AccountAuthModeAPIKey, agent.CatalogFresh)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/", nil)
	value, err := (&GetAIAccountThinking{AccountID: id, RuntimeID: constants.RuntimeIDCodex, Model: "account-model"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	thinking := value.(GetAIAccountThinkingResponse)
	if !thinking.Available || !reflect.DeepEqual(thinking.Options, []AccountThinkingOption{{ID: "high", Name: "High"}}) {
		t.Fatalf("API key used a catalog other than its own: %+v", thinking)
	}
}
