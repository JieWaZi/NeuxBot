package status_error

import "net/http"

const (
	// @errZH API 集成名称、有效期或授权范围无效
	// @errEN integration name, expiration or grants are invalid
	IntegrationInvalid StatusError = http.StatusBadRequest*1e8 + 9601
	// @errZH API Key 无效、已到期或已撤销
	// @errEN integration credential is invalid, expired or revoked
	IntegrationUnauthorized StatusError = http.StatusUnauthorized*1e8 + 9601
	// @errZH 当前 API Key 没有访问该资源或操作的权限
	// @errEN integration principal is not authorized for this resource or operation
	IntegrationForbidden StatusError = http.StatusForbidden*1e8 + 9601
	// @errZH API 集成配置已变化，请刷新后重试
	// @errEN integration revision or request identity conflicts
	IntegrationConflict StatusError = http.StatusConflict*1e8 + 9601
	// @errZH 未找到对应的 API 集成记录
	// @errEN integration record was not found
	IntegrationNotFound StatusError = http.StatusNotFound*1e8 + 9601
	// @errZH 无法生成 API Key，请稍后重试
	// @errEN integration credential generation failed
	IntegrationCredentialError StatusError = http.StatusInternalServerError*1e8 + 9601
	// @errZH API 服务监听或配置保存失败，原配置仍然有效
	// @errEN integration listener or configuration update failed
	IntegrationListenerError StatusError = http.StatusServiceUnavailable*1e8 + 9601
)
