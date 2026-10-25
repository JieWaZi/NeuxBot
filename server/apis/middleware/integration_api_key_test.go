package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	rules "github.com/JieWaZi/NeuxBot/server/internal/integration"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	integrationservice "github.com/JieWaZi/NeuxBot/server/services/integration"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestIntegrationRouteDefaultDenyAndAllRequired(t *testing.T) {
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "api.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB.DB()
	t.Cleanup(func() { _ = pool.Close() })
	store := controllers.NewIntegrationKeyController(db.DB)
	ctx := context.Background()
	_, _, _ = store.ClaimMutation(ctx, models.IntegrationKeyMutation{RequestID: "fixture", Digest: "fixture", KeyID: "key-a", Action: "create"})
	_, err = store.CompleteMutation(ctx, "fixture", "fixture", models.IntegrationKey{ID: "key-a", Name: "测试", KeyHash: rules.HashKey("fixture-only"), Enabled: true, Grants: []models.IntegrationGrant{{ResourceKind: constants.IntegrationResourceKindKnowledge, ResourceIDs: []string{"a"}, Capabilities: []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve}}}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	authorizer := NewAPIKeyRouteAuthorizer()
	authorizer.Register("GET", "/api/v1/knowledge-bases/:id", APIKeyRoutePolicy{ResourceKind: constants.IntegrationResourceKindKnowledge, ResourceParam: "id", Capabilities: []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve}})
	authorizer.Register("POST", "/api/v1/knowledge-bases/:id/organize", APIKeyRoutePolicy{ResourceKind: constants.IntegrationResourceKindKnowledge, ResourceParam: "id", Capabilities: []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve, constants.IntegrationCapabilityOrganize}})
	router := gin.New()
	router.Use(authorizer.Middleware(integrationservice.New(integrationservice.Config{Keys: store})))
	for _, route := range []struct {
		// method 是测试实际发出的 HTTP 方法。
		method string
		// path 是路由器的实际路径模板。
		path string
	}{{"GET", "/api/v1/knowledge-bases/:id"}, {"POST", "/api/v1/knowledge-bases/:id/organize"}, {"GET", "/forgotten"}} {
		router.Handle(route.method, route.path, func(c *gin.Context) { c.Status(http.StatusOK) })
	}
	for _, test := range []struct {
		// method 是请求方法。
		method string
		// path 是实际资源路径。
		path string
		// token 是测试凭据，空值用于验证缺失主体。
		token string
		// want 是独立于生产代码的预期 HTTP 结果。
		want int
	}{{"GET", "/api/v1/knowledge-bases/a", "fixture-only", 200}, {"GET", "/api/v1/knowledge-bases/b", "fixture-only", 403}, {"POST", "/api/v1/knowledge-bases/a/organize", "fixture-only", 403}, {"GET", "/forgotten", "fixture-only", 403}, {"GET", "/api/v1/knowledge-bases/a", "", 401}} {
		request := httptest.NewRequest(test.method, test.path, nil)
		if test.token != "" {
			request.Header.Set("X-API-Key", test.token)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != test.want {
			t.Fatalf("%s %s: %d %s", test.method, test.path, response.Code, response.Body.String())
		}
	}
}
