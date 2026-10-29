package catalog

import (
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"

	acp "github.com/JieWaZi/acp-go/sdk"
)

// piACPRuntimeBinding 组合 Pi 自身的思考目录与产品策略。
func piACPRuntimeBinding() acpRuntimeBinding {
	binding := nativeACPRuntimeBinding()
	binding.baseRuntimeID = runtimecontract.RuntimeIDPi
	binding.displayName = "Pi"
	binding.permissionLevels = func([]acp.SessionConfigOption, []acp.SessionMode) []runtimecontract.PermissionLevel {
		return []runtimecontract.PermissionLevel{runtimecontract.PermissionLevelDefault, runtimecontract.PermissionLevelAuto, runtimecontract.PermissionLevelFullAccess}
	}
	binding.supportsMCPTransport = allMCPTransports

	return binding
}
