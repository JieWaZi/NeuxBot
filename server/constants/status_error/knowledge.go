package status_error

import "net/http"

const (
	// @errZH 请先配置此知识库的独立嵌入服务
	// @errEN configure this library's embedding service first
	KnowledgeEmbeddingNotConfigured StatusError = http.StatusUnprocessableEntity*1e8 + 9503
	// @errZH 知识库嵌入服务暂时不可用
	// @errEN knowledge embedding service unavailable
	KnowledgeEmbeddingUnavailable StatusError = http.StatusBadGateway*1e8 + 9601
	// @errZH 知识库嵌入服务返回无效向量
	// @errEN invalid knowledge embedding response
	KnowledgeEmbeddingResponseInvalid StatusError = http.StatusBadGateway*1e8 + 9602

	// @errZH 知识请求参数不合法
	// @errEN invalid knowledge request
	KnowledgeInvalid StatusError = http.StatusBadRequest*1e8 + 9501
	// @errZH 目录下还有子目录，请先删除子目录
	// @errEN folder contains subfolders; delete them first
	KnowledgeFolderHasChildren StatusError = http.StatusConflict*1e8 + 9503
	// @errZH 目录下还有页面，请先移出页面
	// @errEN folder contains pages; move them out first
	KnowledgeFolderHasPages StatusError = http.StatusConflict*1e8 + 9504
	// @errZH 知识资源不存在
	// @errEN knowledge resource not found
	KnowledgeNotFound StatusError = http.StatusNotFound*1e8 + 9501
	// @errZH 知识版本或请求内容已变化，请刷新后重试
	// @errEN knowledge version or request content has changed
	KnowledgeConflict StatusError = http.StatusConflict*1e8 + 9501
	// @errZH 当前执行无权访问此知识资源或操作
	// @errEN knowledge resource or operation is outside the execution scope
	KnowledgeForbidden StatusError = http.StatusForbidden*1e8 + 9501
	// @errZH 当前知识操作已取消或阶段已失效
	// @errEN knowledge operation was cancelled or its attempt is stale
	KnowledgeCancelled StatusError = http.StatusConflict*1e8 + 9502
	// @errZH 知识执行或候选未满足发布条件
	// @errEN knowledge execution or candidate is not eligible for publication
	KnowledgeCandidateInvalid StatusError = http.StatusUnprocessableEntity*1e8 + 9501
	// @errZH 当前知识服务或智能体不支持此操作
	// @errEN knowledge provider or runtime does not support this operation
	KnowledgeUnsupported StatusError = http.StatusUnprocessableEntity*1e8 + 9502
	// @errZH 知识存储访问失败
	// @errEN knowledge storage failed
	KnowledgeStorageError StatusError = http.StatusInternalServerError*1e8 + 9501
)
