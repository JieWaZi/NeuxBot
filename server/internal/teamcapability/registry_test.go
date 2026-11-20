package teamcapability

import "testing"

func TestRegistrySupportsParallelLeaseScopedCapabilities(t *testing.T) {
	registry := NewRegistry()
	firstLease, err := registry.Mint("thread", "lead", "execution-1", true)
	if err != nil {
		t.Fatalf("mint first scope: %v", err)
	}
	first, exists := registry.Resolve("thread")
	if !exists || first.ExecutionID != "execution-1" || first.Fence == 0 {
		t.Fatalf("first scope = %+v, exists=%t", first, exists)
	}
	secondLease, err := registry.Mint("thread", "lead", "execution-2", false)
	if err != nil {
		t.Fatalf("mint second scope: %v", err)
	}
	registry.Revoke(firstLease)
	second, exists := registry.Resolve("thread")
	if !exists || second.ExecutionID != "execution-2" || second.Fence <= first.Fence {
		t.Fatalf("old lease revoked current scope: %+v, exists=%t", second, exists)
	}
	registry.MarkListed("thread", firstLease)
	second, _ = registry.Resolve("thread")
	if second.Listed {
		t.Fatal("old lease marked the new turn's task board as read")
	}
	memberLease, err := registry.MintMember(
		"thread", "member", "execution-3", "task", "attempt",
	)
	if err != nil {
		t.Fatalf("mint member scope: %v", err)
	}
	member, exists := registry.Resolve(MemberEndpointKey("thread", "member"))
	if !exists || member.Role != "member" || member.TaskID != "task" ||
		member.AttemptID != "attempt" {
		t.Fatalf("member scope = %+v, exists=%t", member, exists)
	}
	registry.Revoke(secondLease)
	if _, exists := registry.Resolve("thread"); exists {
		t.Fatal("current lease did not revoke its scope")
	}
	if _, exists := registry.Resolve(MemberEndpointKey("thread", "member")); !exists {
		t.Fatal("revoking lead scope removed parallel member scope")
	}
	registry.Revoke(memberLease)
}
