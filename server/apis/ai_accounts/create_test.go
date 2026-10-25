package ai_accounts

import (
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestCreateAIAccountNormalizesNameAndRequiresOneProtocol(t *testing.T) {
	request := CreateAIAccount{Body: CreateAIAccountRequest{
		Name: " OpenAI ", AuthMode: constants.AccountAuthModeAPIKey, APIKey: "test-key",
		Endpoints: []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: "https://api.openai.com/v1"}},
	}}
	if err := request.Validate(nil); err != nil {
		t.Fatal(err)
	}
	if request.Body.Name != "OpenAI" {
		t.Fatal("account name was not normalized")
	}
	request.Body.Endpoints = append(request.Body.Endpoints, models.AIAccountEndpoint{Protocol: constants.AccountProtocolAnthropicMessages, BaseURL: "https://api.anthropic.com"})
	if err := request.Validate(nil); !errors.Is(err, status_error.AIAccountConfigurationInvalid) {
		t.Fatalf("multiple protocols accepted: %v", err)
	}
}

// TestGoogleAccountCreateAndUpdatePreserveNativeChoices 验证新增与更新接受并保留实际原生选择 ID。
func TestGoogleAccountCreateAndUpdatePreserveNativeChoices(t *testing.T) {
	levels := map[string][]string{"org/model": {"LOW", "budget:8192", "provider-fast"}}
	endpoint := []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolGoogleGenerativeAI, BaseURL: constants.AIAccountGoogleURL}}
	create := CreateAIAccount{Body: CreateAIAccountRequest{Name: "Google", AuthMode: constants.AccountAuthModeAPIKey, APIKey: "fixture", Endpoints: endpoint, SelectedModels: []string{"org/model"}, ModelReasoningLevels: levels, DefaultModel: "org/model", DefaultReasoning: "budget:8192"}}
	if err := create.Validate(nil); err != nil {
		t.Fatal(err)
	}
	update := UpdateAIAccount{Body: UpdateAIAccountRequest{Name: create.Body.Name, Endpoints: endpoint, SelectedModels: create.Body.SelectedModels, ModelReasoningLevels: create.Body.ModelReasoningLevels, DefaultModel: create.Body.DefaultModel, DefaultReasoning: create.Body.DefaultReasoning}}
	if err := update.Validate(nil); err != nil {
		t.Fatal(err)
	}
	if update.Body.ModelReasoningLevels["org/model"][1] != "budget:8192" || update.Body.DefaultReasoning != "budget:8192" {
		t.Fatal("opaque native choice was normalized")
	}
}
