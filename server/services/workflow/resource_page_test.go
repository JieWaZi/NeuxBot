package workflow

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestCreationResourcePagesExposeEnforcedPoliciesAndBoundConnectors(t *testing.T) {
	service, _ := workflowTestService(t)
	service.wakers = resourceWakerFixture{}
	service.connectors = fixtureConnectors{connector: models.MCPConnector{ID: "bound", Name: "已绑定工具", URL: "https://private.invalid", Headers: []models.MCPSecretDefinition{{ID: "auth", Name: "Authorization", Value: "private-token"}}}}
	ctx := context.Background()
	definition, err := fixtureDefinition(ctx, service.store, definitionFixture{RequestID: "resources", CreationWakerID: "fixture-ally"})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"wakers", "toolPolicyCapabilities"} {
		page, err := service.resourcePage(ctx, definition.ID, kind, "")
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(page)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(encoded), `"nativeTools":["inherit"]`) || !strings.Contains(string(encoded), `"connectors":["inherit","allowlist"]`) {
			t.Fatalf("actual policy capabilities absent from %s: %s", kind, encoded)
		}
		if kind == "wakers" && (!strings.Contains(string(encoded), `"id":"bound"`) || !strings.Contains(string(encoded), definition.CreationWakerID) || !strings.Contains(string(encoded), `"name":"NeuxBot"`)) {
			t.Fatalf("bound connector or reused NeuxBot partner absent: %s", encoded)
		}
		if strings.Contains(string(encoded), "private-token") || strings.Contains(string(encoded), "private.invalid") {
			t.Fatal("resource page exposed connector transport credentials")
		}
	}
}

// resourceWakerFixture 仅提供资源目录测试所需的伙伴记录。
type resourceWakerFixture struct {
	// WakerController 是本场景未使用的仓储方法边界。
	controllers.WakerController
}

func (resourceWakerFixture) List(context.Context, string, int, int) ([]models.Waker, error) {
	return []models.Waker{{ID: "fixture-ally", Name: "NeuxBot"}}, nil
}
