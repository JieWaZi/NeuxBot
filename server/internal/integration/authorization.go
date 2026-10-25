package integration

import (
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// NormalizeGrants 适配 WeKnora 的能力集合乘资源集合模型，拒绝空范围和未知能力。
func NormalizeGrants(in []models.IntegrationGrant) ([]models.IntegrationGrant, error) {
	if len(in) == 0 || len(in) > 2 {
		return nil, status_error.IntegrationInvalid
	}
	out := make([]models.IntegrationGrant, 0, len(in))
	seen := map[constants.IntegrationResourceKind]bool{}
	for _, g := range in {
		if seen[g.ResourceKind] {
			return nil, status_error.IntegrationInvalid
		}
		seen[g.ResourceKind] = true
		ids := normalizeIDs(g.ResourceIDs)
		if len(ids) == 0 || len(ids) > 1000 || len(g.Capabilities) == 0 {
			return nil, status_error.IntegrationInvalid
		}
		for _, id := range ids {
			if len(id) > 200 || strings.ContainsAny(id, "*\\/\x00\r\n") {
				return nil, status_error.IntegrationInvalid
			}
		}
		capabilities := []constants.IntegrationCapability{}
		for _, capability := range g.Capabilities {
			valid := g.ResourceKind == constants.IntegrationResourceKindKnowledge && (capability == constants.IntegrationCapabilityRetrieve || capability == constants.IntegrationCapabilityIngest || capability == constants.IntegrationCapabilityOrganize)
			valid = valid || g.ResourceKind == constants.IntegrationResourceKindAutomaticTask && (capability == constants.IntegrationCapabilityTaskInvoke || capability == constants.IntegrationCapabilityTaskResults)
			if !valid {
				return nil, status_error.IntegrationInvalid
			}
			if !slices.Contains(capabilities, capability) {
				capabilities = append(capabilities, capability)
			}
		}
		if g.ResourceKind == constants.IntegrationResourceKindKnowledge && !slices.Contains(capabilities, constants.IntegrationCapabilityRetrieve) {
			return nil, status_error.IntegrationInvalid
		}
		slices.Sort(ids)
		slices.Sort(capabilities)
		out = append(out, models.IntegrationGrant{ResourceKind: g.ResourceKind, ResourceIDs: ids, Capabilities: capabilities})
	}
	slices.SortFunc(out, func(a, b models.IntegrationGrant) int {
		return strings.Compare(string(a.ResourceKind), string(b.ResourceKind))
	})
	return out, nil
}

// Allows 检查实际资源和全部必要能力，空要求与空范围均不放行。
func Allows(grants []models.IntegrationGrant, kind constants.IntegrationResourceKind, resourceID string, required ...constants.IntegrationCapability) bool {
	if resourceID == "" || len(required) == 0 {
		return false
	}
	for _, g := range grants {
		if g.ResourceKind != kind || !slices.Contains(g.ResourceIDs, resourceID) {
			continue
		}
		for _, capability := range required {
			if !slices.Contains(g.Capabilities, capability) {
				return false
			}
		}
		return true
	}
	return false
}
