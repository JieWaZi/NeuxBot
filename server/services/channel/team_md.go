package channel

import (
	"context"
	"fmt"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// teamInstruction 为一次团队相关执行生成虚拟 TEAM.md 指令段。
func (s *service) teamInstruction(
	ctx context.Context,
	thread models.ChannelThread,
	selfWakerID string,
) (agentapi.Instruction, error) {
	content, err := s.buildTeamMD(ctx, thread, selfWakerID)
	if err != nil {
		return agentapi.Instruction{}, err
	}
	return agentapi.Instruction{
		Name:    constants.TeamInstructionFileName,
		Content: content,
	}, nil
}

// leaderTurnInstruction 仅描述当前负责人的执行模式，不改变稳定 TEAM 或执行权限。
func leaderTurnInstruction(directExecution bool) workinput.Instruction {
	instruction := workinput.Instruction{Name: agentapi.TeamLeadTurnName, Content: agentapi.TeamLeadTurnIntroduction}
	if directExecution {
		instruction.Content += agentapi.TeamLeadDirectInstructions
	} else {
		instruction.Content += agentapi.TeamLeadCoordinationInstructions
	}
	return instruction
}

// buildTeamMD 按负责人/成员角色组装紧凑的虚拟 TEAM.md，明确角色职责与协作边界。
func (s *service) buildTeamMD(
	ctx context.Context,
	thread models.ChannelThread,
	selfWakerID string,
) (string, error) {
	channel, err := s.channels.Get(ctx, thread.ChannelID)
	if err != nil {
		return "", err
	}
	members, err := s.currentThreadMemberProfiles(ctx, thread.ID)
	if err != nil {
		return "", err
	}
	selfIsLead := selfWakerID == thread.LeadWakerID
	selfRole := "Member"
	if selfIsLead {
		selfRole = "Lead"
	}
	var builder strings.Builder
	builder.WriteString(agentapi.TeamCollaborationInstructions)
	builder.WriteString("# Team: ")
	builder.WriteString(strings.TrimSpace(channel.Name))
	builder.WriteString("\n")
	if title := strings.TrimSpace(thread.Title); title != "" {
		builder.WriteString("Chat: ")
		builder.WriteString(title)
		builder.WriteString("\n")
	}
	builder.WriteString("Role: ")
	builder.WriteString(selfRole)
	builder.WriteString("\n")
	if section := teamWorkspaceSection(thread.WorkspaceDirectory); section != "" {
		builder.WriteString("\n")
		builder.WriteString(section)
		builder.WriteString("\n")
	}
	roster := memberRoster(members, selfWakerID, selfIsLead)
	builder.WriteString("\n## Members\n")
	builder.WriteString(agentapi.TeamRosterInstructions)
	builder.WriteString(roster)
	if selfIsLead {
		builder.WriteString(agentapi.TeamLeadInstructions)
		fmt.Fprintf(&builder, agentapi.TeamNotificationInstructions, constants.ChannelMemberReportMaximumRunes)
		return builder.String(), nil
	}
	builder.WriteString(agentapi.TeamMemberInstructions)
	return builder.String(), nil
}

// currentThreadMemberProfiles 只刷新已接纳成员的公开档案，缺失伙伴保留历史展示资料。
func (s *service) currentThreadMemberProfiles(ctx context.Context, threadID string) ([]models.ChannelThreadMember, error) {
	members, err := s.threads.ListMembers(ctx, threadID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(members))
	for _, member := range members {
		ids = append(ids, member.WakerID)
	}
	wakers, err := s.wakers.ListByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	profiles := make(map[string]models.Waker, len(wakers))
	for _, waker := range wakers {
		profiles[waker.ID] = waker
	}
	for index := range members {
		waker, exists := profiles[members[index].WakerID]
		if !exists {
			continue
		}
		members[index].DisplayName = rosterText(waker.Name, constants.ChannelRosterMaximumNameRunes, "Not provided")
		members[index].RoleName = rosterText(waker.RoleName, constants.ChannelRosterMaximumRoleNameRunes, "")
		members[index].Bio = rosterText(waker.Bio, constants.ChannelRosterMaximumBioRunes, "")
	}
	return members, nil
}

// memberRoster 组装有界成员列表；负责人看到完整分派摘要，成员看到简表。
func memberRoster(
	members []models.ChannelThreadMember,
	selfWakerID string,
	detailed bool,
) string {
	var roster strings.Builder
	for _, member := range members {
		name := rosterText(member.DisplayName, constants.ChannelRosterMaximumNameRunes, "Not provided")
		teamRole := "Member"
		if member.IsLead {
			teamRole = "Lead (cannot be assigned tasks)"
		}
		if !detailed {
			if member.WakerID == selfWakerID {
				roster.WriteString(fmt.Sprintf("- **you** `%s` (%s)\n", member.WakerID, teamRole))
			} else {
				roster.WriteString(fmt.Sprintf("- **%s** `%s` (%s)\n", name, member.WakerID, teamRole))
			}
			continue
		}
		roleName := rosterText(member.RoleName, constants.ChannelRosterMaximumRoleNameRunes, "Not provided")
		frontmatter := rosterText(member.Bio, constants.ChannelRosterMaximumBioRunes, "")
		if member.WakerID == selfWakerID {
			roster.WriteString(fmt.Sprintf("- **you** `%s` (%s)\n  Role: %s", member.WakerID, teamRole, roleName))
		} else {
			roster.WriteString(fmt.Sprintf(
				"- **%s** `%s` (%s)\n  Role: %s",
				name,
				member.WakerID,
				teamRole,
				roleName,
			))
		}
		if frontmatter != "" {
			roster.WriteString("\n  ")
			roster.WriteString(frontmatter)
		}
		roster.WriteString("\n")
	}
	return roster.String()
}

// snapshotMemberExpertise 提取有界专长摘要，供团队成员的历史展示使用。
func snapshotMemberExpertise(expertise []profile.Expertise) []profile.Expertise {
	result := make([]profile.Expertise, 0, constants.ChannelRosterMaximumExpertiseCount)
	for _, item := range expertise {
		if len(result) >= constants.ChannelRosterMaximumExpertiseCount {
			break
		}
		title := rosterText(item.Title, constants.ChannelRosterMaximumExpertiseTitleRunes, "")
		description := rosterText(item.Description, constants.ChannelRosterMaximumExpertiseDescriptionRunes, "")
		if title == "" && description == "" {
			continue
		}
		result = append(result, profile.Expertise{Title: title, Description: description})
	}
	return result
}

// teamWorkspaceSection 生成共享执行工作区说明；路径为空时不注入。
func teamWorkspaceSection(workspaceDirectory string) string {
	path := strings.TrimSpace(workspaceDirectory)
	if path == "" {
		return ""
	}
	return fmt.Sprintf(agentapi.TeamWorkspaceInstructions, path)
}

// rosterText 把成员公开资料压成单行并按字符数截断，避免破坏 roster 结构。
func rosterText(value string, maximumRunes int, fallback string) string {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if normalized == "" || maximumRunes <= 0 {
		return fallback
	}
	runes := []rune(normalized)
	if len(runes) <= maximumRunes {
		return normalized
	}
	return string(runes[:maximumRunes-1]) + "…"
}
