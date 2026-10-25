package status_error

import "net/http"

const (
	// @errZH 远端知识服务连接失败
	// @errEN remote knowledge service is unreachable
	KnowledgeRemoteUnreachable StatusError = http.StatusBadGateway*1e8 + 9501
	// @errZH 远端知识服务响应不符合支持的协议
	// @errEN remote knowledge service protocol is incompatible
	KnowledgeRemoteIncompatible StatusError = http.StatusBadGateway*1e8 + 9502
	// @errZH 远端知识服务认证失败，请检查连接凭据
	// @errEN remote knowledge authentication failed
	KnowledgeRemoteUnauthorized StatusError = http.StatusUnauthorized*1e8 + 9501
	// @errZH 远端操作结果待确认，请使用原请求查询回执
	// @errEN remote operation outcome is unknown; inspect the original request receipt
	KnowledgeRemoteOutcomeUnknown StatusError = http.StatusGatewayTimeout*1e8 + 9501
)
