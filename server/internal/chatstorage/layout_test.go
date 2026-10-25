package chatstorage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
)

func TestLayoutSeparatesTeamMemberCWDAndSharesProjects(t *testing.T) {
	layout, err := NewLayout(t.TempDir())
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	threadID := "thread-1"
	lead, err := layout.TeamMemberWorkspace(threadID, "lead-1", true)
	if err != nil {
		t.Fatalf("TeamMemberWorkspace(lead) error = %v", err)
	}
	member, err := layout.TeamMemberWorkspace(threadID, "member-1", false)
	if err != nil {
		t.Fatalf("TeamMemberWorkspace(member) error = %v", err)
	}
	projects, err := layout.TeamProjects(threadID)
	if err != nil {
		t.Fatalf("TeamProjects() error = %v", err)
	}
	if lead == member {
		t.Fatalf("lead and member cwd must differ: %q", lead)
	}
	if filepath.Base(lead) != teamLeaderDirectoryName || filepath.Base(member) != "member-1" {
		t.Fatalf("unexpected member cwd: lead=%q member=%q", lead, member)
	}
	if filepath.Base(projects) != projectsDirectoryName {
		t.Fatalf("projects directory = %q", projects)
	}
}

func TestLayoutPublishesAndRollsBackTeamProvisioning(t *testing.T) {
	layout, err := NewLayout(t.TempDir())
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	threadID := "thread-provisioning"
	provisioningID := "provisioning-1"
	staging, err := layout.TeamProvisioningLayout(provisioningID)
	if err != nil {
		t.Fatalf("TeamProvisioningLayout() error = %v", err)
	}
	stagedWorkspace, _ := staging.TeamWorkspace(threadID)
	if err := workspace.EnsureThreadExecutionWorkspace(
		stagedWorkspace,
		threadID,
		"lead-1",
		nil,
	); err != nil {
		t.Fatalf("EnsureThreadExecutionWorkspace() error = %v", err)
	}
	if err := layout.PublishTeamProvisioning(provisioningID, threadID); err != nil {
		t.Fatalf("PublishTeamProvisioning() error = %v", err)
	}
	finalWorkspace, _ := layout.TeamWorkspace(threadID)
	if _, err := os.Stat(finalWorkspace); err != nil {
		t.Fatalf("published workspace stat error = %v", err)
	}
	if _, err := os.Stat(stagedWorkspace); !os.IsNotExist(err) {
		t.Fatalf("staged workspace still exists after publish: %v", err)
	}

	rollbackThreadID := "thread-rollback"
	rollbackProvisioningID := "provisioning-2"
	rollbackLayout, _ := layout.TeamProvisioningLayout(rollbackProvisioningID)
	rollbackWorkspace, _ := rollbackLayout.TeamWorkspace(rollbackThreadID)
	if err := workspace.EnsureThreadExecutionWorkspace(
		rollbackWorkspace,
		rollbackThreadID,
		"lead-1",
		nil,
	); err != nil {
		t.Fatalf("prepare rollback workspace: %v", err)
	}
	if err := layout.RollbackTeamProvisioning(rollbackProvisioningID, rollbackThreadID); err != nil {
		t.Fatalf("RollbackTeamProvisioning() error = %v", err)
	}
	if _, err := os.Stat(rollbackWorkspace); !os.IsNotExist(err) {
		t.Fatalf("staged workspace still exists after rollback: %v", err)
	}
}

func TestLayoutRollsBackWorkspaceCreatedBeforeOwnerMarker(t *testing.T) {
	layout, err := NewLayout(t.TempDir())
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	threadID := "thread-incomplete-marker"
	provisioningID := "provisioning-incomplete-marker"
	staging, err := layout.TeamProvisioningLayout(provisioningID)
	if err != nil {
		t.Fatalf("TeamProvisioningLayout() error = %v", err)
	}
	workspaceRoot, err := staging.TeamWorkspace(threadID)
	if err != nil {
		t.Fatalf("TeamWorkspace() error = %v", err)
	}
	if err := os.MkdirAll(workspaceRoot, 0o700); err != nil {
		t.Fatalf("create incomplete workspace: %v", err)
	}
	if err := layout.RollbackTeamProvisioning(provisioningID, threadID); err != nil {
		t.Fatalf("RollbackTeamProvisioning() error = %v", err)
	}
	teamRoot, _ := staging.TeamRoot(threadID)
	if _, err := os.Stat(teamRoot); !os.IsNotExist(err) {
		t.Fatalf("incomplete provisioning root still exists: %v", err)
	}
}

func TestCatalogDiscoversOrdinaryAndTeamLogs(t *testing.T) {
	root := t.TempDir()
	layout, err := NewLayout(root)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	ordinaryID := "0123456789abcdef0123456789abcdef"
	teamID := "fedcba9876543210fedcba9876543210"
	threadID := "thread-1"
	ordinaryLog, _ := layout.ChatLogDirectory(ordinaryID)
	teamLog, _ := layout.TeamLogDirectory(threadID)
	for _, directory := range []string{ordinaryLog, teamLog} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", directory, err)
		}
	}
	if err := os.WriteFile(filepath.Join(ordinaryLog, ordinaryID+".jsonl"), nil, 0o600); err != nil {
		t.Fatalf("write ordinary log: %v", err)
	}
	if err := os.WriteFile(filepath.Join(teamLog, teamID+".jsonl"), nil, 0o600); err != nil {
		t.Fatalf("write team log: %v", err)
	}
	catalog, err := NewCatalog(layout)
	if err != nil {
		t.Fatalf("NewCatalog() error = %v", err)
	}
	ids := catalog.ListChatIDs()
	if len(ids) != 2 || ids[0] != ordinaryID || ids[1] != teamID {
		t.Fatalf("ListChatIDs() = %#v", ids)
	}
	teamAddress, err := catalog.Address(teamID)
	if err != nil || teamAddress.ThreadID != threadID {
		t.Fatalf("team address = %+v, error = %v", teamAddress, err)
	}
}
