package channel

import (
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/internal/profile"
)

func TestMemberFrontmatterKeepsBoundedPublicProjection(t *testing.T) {
	frontmatter := memberFrontmatter(
		"负责桌面端界面与交互实现。",
		[]profile.Expertise{{
			Title:       "前端交互",
			Description: "设计并实现可访问的桌面端组件。",
		}},
	)
	if !strings.Contains(frontmatter, "负责桌面端界面与交互实现。") ||
		!strings.Contains(frontmatter, "前端交互: 设计并实现可访问的桌面端组件。") {
		t.Fatalf("frontmatter = %q", frontmatter)
	}
	if strings.Contains(frontmatter, "{") || strings.Contains(frontmatter, "[") {
		t.Fatalf("frontmatter unexpectedly embeds JSON: %q", frontmatter)
	}
}

func TestTeamWorkspaceSectionOmitEmptyPath(t *testing.T) {
	if section := teamWorkspaceSection("  "); section != "" {
		t.Fatalf("empty workspace section = %q", section)
	}
	section := teamWorkspaceSection("/tmp/shared")
	if !strings.Contains(section, "/tmp/shared") || !strings.Contains(section, "Team Workspace") {
		t.Fatalf("workspace section = %q", section)
	}
}
