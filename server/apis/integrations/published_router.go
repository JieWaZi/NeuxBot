package integrations

import (
	contract "github.com/JieWaZi/NeuxAlly/server"
	"github.com/JieWaZi/NeuxAlly/server/apis/automatic_tasks"
	"github.com/JieWaZi/NeuxAlly/server/apis/integration_knowledge"
	knowledgemcp "github.com/JieWaZi/NeuxAlly/server/apis/integration_mcp"
	"github.com/JieWaZi/NeuxAlly/server/global"
	"github.com/JieWaZi/NeuxAlly/server/services/schedule"
	"net/http"
	"strings"
)

// NewPublishedHandler 只装配已声明的知识和自动任务合同，只接受统一 API Key。
func NewPublishedHandler(service *schedule.Service, config schedule.Config, address string) http.Handler {
	publisher := global.IntegrationAPIListener
	knowledge := integration_knowledge.NewHandler(global.KnowledgeEngine, global.KnowledgeService, global.IntegrationService, integration_knowledge.Options{References: global.KnowledgeControllerManager, Events: global.EventHub})
	mcp := knowledgemcp.NewHandler(global.KnowledgeService, global.IntegrationService)
	tasks := automatic_tasks.NewIntegrationHandler(service, config, global.IntegrationService)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if publisher == nil {
			http.NotFound(w, r)
			return
		}
		settings := publisher.Settings()
		if r.URL.Path == "/mcp" {
			if !settings.Enabled || !settings.MCPEnabled {
				http.NotFound(w, r)
				return
			}
		} else if !settings.Enabled {
			http.NotFound(w, r)
			return
		}
		switch {
		case r.URL.Path == "/mcp":
			mcp.ServeHTTP(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/integration-openapi.json":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			_, _ = w.Write(contract.IntegrationOpenAPI)
		case strings.HasPrefix(r.URL.Path, "/api/v1/automatic-tasks/"):
			tasks.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, "/api/v1/knowledge-bases"), strings.HasPrefix(r.URL.Path, "/api/v1/knowledgebase/"), strings.HasPrefix(r.URL.Path, "/api/v1/knowledge/"):
			knowledge.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}
