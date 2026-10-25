package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestIntegrationKeyGenerationAndDigest(t *testing.T) {
	a, err := GenerateKey()
	if err != nil || !strings.HasPrefix(a, "sk-") || len(a) != 46 {
		t.Fatalf("key shape: %q %v", a, err)
	}
	b, err := GenerateKey()
	if err != nil || a == b {
		t.Fatal("keys are not independent", err)
	}
	if HashKey("fixture-only") != "2b9cc4a9db471dbe900f09fbe469af30efdd8a740f938e325120903fb274339d" {
		t.Fatal("credential digest contract changed")
	}
}

func TestIntegrationGrantEmptyAndUnknown(t *testing.T) {
	for _, grants := range [][]models.IntegrationGrant{
		nil,
		{{ResourceKind: constants.IntegrationResourceKindKnowledge, Capabilities: []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve}}},
		{{ResourceKind: constants.IntegrationResourceKindKnowledge, ResourceIDs: []string{"a"}, Capabilities: []constants.IntegrationCapability{"future_admin"}}},
		{{ResourceKind: constants.IntegrationResourceKindKnowledge, ResourceIDs: []string{"a"}, Capabilities: []constants.IntegrationCapability{constants.IntegrationCapabilityIngest}}},
	} {
		if _, err := NormalizeGrants(grants); err == nil {
			t.Fatalf("invalid grant accepted: %#v", grants)
		}
	}
	grants, err := NormalizeGrants([]models.IntegrationGrant{{ResourceKind: constants.IntegrationResourceKindKnowledge, ResourceIDs: []string{" a ", "a", "b"}, Capabilities: []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve, constants.IntegrationCapabilityOrganize}}})
	if err != nil || len(grants[0].ResourceIDs) != 2 {
		t.Fatal(grants, err)
	}
	if !Allows(grants, constants.IntegrationResourceKindKnowledge, "a", constants.IntegrationCapabilityRetrieve, constants.IntegrationCapabilityOrganize) || Allows(grants, constants.IntegrationResourceKindKnowledge, "a", constants.IntegrationCapabilityIngest) || Allows(grants, constants.IntegrationResourceKindKnowledge, "c", constants.IntegrationCapabilityRetrieve) {
		t.Fatal("grant escaped operation or exact resource scope")
	}
	if Allows(nil, constants.IntegrationResourceKindKnowledge, "a", constants.IntegrationCapabilityRetrieve) {
		t.Fatal("empty grant allowed access")
	}
	encoded, _ := json.Marshal(models.IntegrationKey{KeyHash: "secret-digest"})
	if strings.Contains(string(encoded), "secret-digest") {
		t.Fatal("key digest leaked")
	}
}
