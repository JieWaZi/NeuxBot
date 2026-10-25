package middleware

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	integrationservice "github.com/JieWaZi/NeuxBot/server/services/integration"
	"github.com/gin-gonic/gin"
)

// Copyright (C) 2026 Tencent. MIT，许可见 THIRD_PARTY_NOTICES.md。

// APIKeyRoutePolicy 声明单一路由的全部权限及实际资源定位方式。
type APIKeyRoutePolicy struct {
	// ResourceKind 是该路由操作的资源类型。
	ResourceKind constants.IntegrationResourceKind
	// ResourceParam 指定直接包含实际资源 ID 的路径参数。
	ResourceParam string
	// ResolveResource 在资料等间接路径中解析真实所属库，不能由正文自报。
	ResolveResource func(*gin.Context) (string, error)
	// Capabilities 是该路由必须同时满足的全部能力。
	Capabilities []constants.IntegrationCapability
	// Collection 表示按授权 ID 集合过滤的列表，不授权任意库。
	Collection bool
	// IdentityOnly 仅供显式登记的不含资源信息的公开合同文档使用。
	IdentityOnly bool
}

// APIKeyRouteAuthorizer 在构建路由时登记策略，请求期间只读。
type APIKeyRouteAuthorizer struct {
	// policies 按 HTTP 方法和 Gin 实际路径模板保存策略。
	policies map[string]map[string]APIKeyRoutePolicy
}

// NewAPIKeyRouteAuthorizer 创建默认拒绝全部路由的登记表。
func NewAPIKeyRouteAuthorizer() *APIKeyRouteAuthorizer {
	return &APIKeyRouteAuthorizer{policies: map[string]map[string]APIKeyRoutePolicy{}}
}

// Register 登记规范化路由，并复制所需能力以隔离调用方后续修改。
func (a *APIKeyRouteAuthorizer) Register(method, fullPath string, policy APIKeyRoutePolicy) {
	method = strings.ToUpper(strings.TrimSpace(method))
	fullPath = normalizeRoutePath(fullPath)
	if a.policies[method] == nil {
		a.policies[method] = map[string]APIKeyRoutePolicy{}
	}
	policy.Capabilities = slices.Clone(policy.Capabilities)
	a.policies[method][fullPath] = policy
}

// Lookup 只接受已登记的方法及真实路径模板。
func (a *APIKeyRouteAuthorizer) Lookup(method, fullPath string) (APIKeyRoutePolicy, bool) {
	byPath, ok := a.policies[strings.ToUpper(method)]
	if !ok {
		return APIKeyRoutePolicy{}, false
	}
	policy, ok := byPath[normalizeRoutePath(fullPath)]
	return policy, ok
}

// RegisteredRoutes 提供启动自检及生成公开合同的同源路由集合。
func (a *APIKeyRouteAuthorizer) RegisteredRoutes() map[string][]string {
	out := make(map[string][]string, len(a.policies))
	for method, byPath := range a.policies {
		paths := make([]string, 0, len(byPath))
		for path := range byPath {
			paths = append(paths, path)
		}
		slices.Sort(paths)
		out[method] = paths
	}
	return out
}

// integrationPrincipalKey 保证受信身份无法被任意字符串字段覆盖。
type integrationPrincipalKey struct{}

// integrationAuthorizationKey 保存当前路由已确认的完整目标。
type integrationAuthorizationKey struct{}

// IntegrationPrincipal 返回 HTTP 边界写入的主体，不读取客户端自报字段。
func IntegrationPrincipal(ctx context.Context) (models.IntegrationIdentity, bool) {
	value, ok := ctx.Value(integrationPrincipalKey{}).(models.IntegrationIdentity)
	return value, ok && value.KeyID != ""
}

// IntegrationAuthorization 返回当前操作的原始受信授权。
func IntegrationAuthorization(ctx context.Context) (models.IntegrationAuthorization, bool) {
	value, ok := ctx.Value(integrationAuthorizationKey{}).(models.IntegrationAuthorization)
	return value, ok && value.Identity.KeyID != ""
}

// AbortIntegration 输出稳定错误码，不包含凭据、底层错误或远端响应正文。
func AbortIntegration(ctx *gin.Context, err error) {
	var coded interface {
		error
		StatusCode() int
	}
	status := http.StatusInternalServerError
	if errors.As(err, &coded) {
		status = coded.StatusCode()
	}
	ctx.Header("Cache-Control", "no-store")
	ctx.AbortWithStatusJSON(status, gin.H{"errorCode": util.ErrorCode(err, status_error.IntegrationForbidden.Key())})
}

// Middleware 强制认证后校验路由策略；缺主体不交给 JWT 或桌面认证放行。
func (a *APIKeyRouteAuthorizer) Middleware(service *integrationservice.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Header("Cache-Control", "no-store")
		ctx.Header("X-Content-Type-Options", "nosniff")
		if ctx.GetHeader("Origin") != "" || ctx.GetHeader("Sec-Fetch-Site") != "" {
			AbortIntegration(ctx, status_error.IntegrationForbidden)
			return
		}
		identity, err := service.Authenticate(ctx.Request.Context(), ctx.GetHeader("X-API-Key"))
		if err != nil {
			AbortIntegration(ctx, err)
			return
		}
		policy, declared := a.Lookup(ctx.Request.Method, ctx.FullPath())
		if !declared {
			AbortIntegration(ctx, status_error.IntegrationForbidden)
			return
		}
		requestContext := context.WithValue(ctx.Request.Context(), integrationPrincipalKey{}, identity)
		ctx.Request = ctx.Request.WithContext(requestContext)
		if !policy.IdentityOnly {
			resource := ctx.Param(policy.ResourceParam)
			if policy.ResolveResource != nil {
				resource, err = policy.ResolveResource(ctx)
			}
			var auth models.IntegrationAuthorization
			if err == nil && policy.Collection {
				var ids []string
				ids, err = service.ResourceIDs(requestContext, identity, policy.ResourceKind, policy.Capabilities)
				if err == nil && len(ids) == 0 {
					err = status_error.IntegrationForbidden
				}
			} else if err == nil {
				auth, err = service.Authorize(requestContext, identity, policy.ResourceKind, resource, policy.Capabilities)
			}
			if err != nil {
				AbortIntegration(ctx, err)
				return
			}
			if auth.Identity.KeyID != "" {
				auth.RemoteAddr = ctx.Request.RemoteAddr
				auth.ForwardedFor = strings.Join(ctx.Request.Header.Values("X-Forwarded-For"), ",")
				requestContext = context.WithValue(requestContext, integrationAuthorizationKey{}, auth)
			}
		}
		ctx.Request = ctx.Request.WithContext(requestContext)
		ctx.Next()
	}
}

func normalizeRoutePath(path string) string {
	if path == "" {
		return ""
	}
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	if len(path) > 1 {
		path = strings.TrimSuffix(path, "/")
	}
	return path
}
