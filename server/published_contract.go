// Package server 保存与实际公开登记同步生成的集成合同。
package server

import _ "embed"

// IntegrationOpenAPI 只包含公开接口和可达模型，不含桌面管理合同。
//
//go:embed integration-openapi.json
var IntegrationOpenAPI []byte
