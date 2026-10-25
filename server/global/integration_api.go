package global

import (
	"context"

	integrationapi "github.com/JieWaZi/NeuxBot/server/internal/integration"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	integrationservice "github.com/JieWaZi/NeuxBot/server/services/integration"
)

var (
	// IntegrationKeyControllerManager 是集成凭据和授权的唯一持久源。
	IntegrationKeyControllerManager controllers.IntegrationKeyController
	// IntegrationService 共享知识与任务入口的认证和提交协调。
	IntegrationService *integrationservice.Service
	// IntegrationAPIListener 是设置页与任务页共享的唯一主监听。
	IntegrationAPIListener *integrationapi.Listener
)

func loadIntegrationAuthorization() {
	IntegrationKeyControllerManager = controllers.NewIntegrationKeyController(DB)
	IntegrationService = integrationservice.New(integrationservice.Config{Keys: IntegrationKeyControllerManager, Tasks: controllers.NewAutomaticTaskController(DB)})
}

func loadIntegrationAPI(ctx context.Context) error {
	if IntegrationAPIListener == nil {
		return nil
	}
	return IntegrationAPIListener.Start(ctx)
}

func closeIntegrationAPI() {
	if IntegrationAPIListener != nil {
		IntegrationAPIListener.Close()
		IntegrationAPIListener = nil
	}
}
