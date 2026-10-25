package channel

import (
	"context"
	"fmt"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// teamInstruction 为一次团队相关执行生成虚拟 TEAM.md 指令段。
func (s *service) teamInstruction(
	ctx context.Context,
	thread models.ChannelThread,
	selfWakerID string,
) (runtimes.Instruction, error) {
	content, err := s.buildTeamMD(ctx, thread, selfWakerID)
	if err != nil {
		return runtimes.Instruction{}, err
	}
	return runtimes.Instruction{
		Name:    constants.TeamInstructionFileName,
		Content: content,
	}, nil
}

// leaderTurnInstruction 将当前负责人回合的路由意图写入虚拟 TEAM.md，不改变执行权限。
func leaderTurnInstruction(instruction runtimes.Instruction, directExecution bool) runtimes.Instruction {
	instruction.Content += "\n\n## Current Lead Turn\nThis section applies only to the current execution and replaces the execution mode from earlier turns.\n"
	if directExecution {
		instruction.Content += "This is a direct-execution turn: the user mentioned you through the structured `@` mechanism. Execute the current request in this Chat. You may delegate independent subtasks to non-lead members, but do not create a team task for yourself."
	} else {
		instruction.Content += "This is a coordination turn: delegate file changes, command execution, or formal deliverables to non-lead members. You may answer explanations, clarifications, and final summaries directly. Do not carry forward direct-execution mode from an earlier turn."
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
	members, err := s.threads.ListMembers(ctx, thread.ID)
	if err != nil {
		return "", err
	}
	selfIsLead := selfWakerID == thread.LeadWakerID
	selfRole := "Member"
	if selfIsLead {
		selfRole = "Lead"
	}
	var builder strings.Builder
	builder.WriteString("## Collaboration Priority\n")
	builder.WriteString("Team coordination rules take precedence over general advice in the partner profile. ")
	builder.WriteString("The lead may perform formal implementation directly in this Chat only when the user mentions the lead through the structured `@` mechanism; that path does not create a task assigned to the lead. ")
	builder.WriteString("Dispatching a task does not mean that the task is complete.\n\n")
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
	builder.WriteString("This is the authoritative list of current team members; do not use tools to verify it again.\n")
	builder.WriteString("Names are for readability only. `assigneeWakerId` must use a Waker ID shown in backticks below; ")
	builder.WriteString("do not use `you`, a member name, or another natural-language alias.\n\n")
	builder.WriteString(roster)
	if selfIsLead {
		builder.WriteString("\n## Workflow\n\n")
		builder.WriteString("You are the sole lead for this team Chat and are responsible for planning, delegation, coordination, and the final summary. ")
		builder.WriteString("This Chat is both the lead session and the main team session. ")
		builder.WriteString("During a normal coordination turn, any file creation or modification, command execution, or formal deliverable must be delegated to a non-lead member by creating a task with `allycli team tasks`. ")
		builder.WriteString("When the user mentions the lead through the structured `@` mechanism, the system sends the original input directly to this Chat as a direct-execution turn. In that turn, you may implement the request yourself or delegate independent subtasks to non-lead members, but you must not create a task for yourself. ")
		builder.WriteString("The final summary is the lead's coordination closeout after receiving member results. Reply to the user directly and do not create a task for it. ")
		builder.WriteString("You may directly handle explanations, clarifications, or short answers that require no formal execution.\n\n")
		builder.WriteString("Rules:\n")
		builder.WriteString("- Call `list` or `search` before creating, updating, or retrying a task to avoid duplicate work\n")
		builder.WriteString("- `assigneeWakerId` must be the Waker ID of a non-lead member from Members; the lead cannot be a task assignee\n")
		builder.WriteString("- Describe exactly one clear deliverable per task; split work that spans specialties into separate tasks\n")
		builder.WriteString("- Match tasks semantically to member roles, biographies, and expertise; structured `@` mentions are already routed by the system, so do not reassign them\n")
		builder.WriteString("- Build the complete task graph in one pass, tell the user work has started, and then end the turn; do not poll or wait for members\n")
		builder.WriteString("- When a single task can answer the user in one complete member report, set `directDelivery=true` on create. Use normal lead review for multiple tasks, dependencies, human review, or work needing synthesis\n")
		builder.WriteString("- Do not claim that team work is complete while the task board contains any pending, in_progress, or in_review task\n")
		builder.WriteString("- Member reports appear directly in this Chat; after the task board settles, the system wakes you only when coordination or synthesis is needed\n")
		builder.WriteString("- Do not act on a truncated report\n")
		builder.WriteString("- Do not expose the internal task board, notification queue, or tool protocol directly to the user\n\n")
		builder.WriteString("## allycli team tasks\n\n")
		builder.WriteString("Read allycli guide team.lead for the method and discover authorized commands with allycli capabilities.\n")
		return builder.String(), nil
	}
	builder.WriteString("\n## Rules\n\n")
	builder.WriteString("As an execution member, focus on the task assigned to you.\n\n")
	builder.WriteString("- Stay within the task scope and do not drift from the assigned objective\n")
	builder.WriteString("- After completing the work and required verification, call `allycli team reports submit` exactly once before your final response\n")
	builder.WriteString("- Put the complete result in `report.text`: findings or changes, verification, and remaining risks. For a direct-delivery task, set `readyForUser=true` only when this text itself is a complete answer the user can read; otherwise let the lead synthesize it\n")
	builder.WriteString("- If blocked, still use `report` to state truthfully what information, permission, or decision is missing\n")
	builder.WriteString("- After the report succeeds, final is one natural acknowledgement sentence. Never repeat the report or write a second complete answer in final\n")
	builder.WriteString("- If `report` is unavailable or still fails after retrying, the final response must instead contain the complete factual report as a factual final response\n")
	builder.WriteString("- The system commits task status automatically after the run ends; no additional completion action is needed\n")
	builder.WriteString("\n## allycli team tasks\n\n")
	builder.WriteString("Read allycli guide team.member and submit the factual report with allycli team reports submit.\n")
	return builder.String(), nil
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
		frontmatter := memberFrontmatter(member.Bio, member.Expertise)
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

// snapshotMemberExpertise 冻结有界专长摘要，避免旧团队依赖后续可变伙伴资料。
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

// memberFrontmatter 把简介与专长压成有界单行摘要，供负责人分派使用。
func memberFrontmatter(bio string, expertise []profile.Expertise) string {
	parts := make([]string, 0, 1+constants.ChannelRosterMaximumExpertiseCount)
	if summary := rosterText(bio, constants.ChannelRosterMaximumBioRunes, ""); summary != "" {
		parts = append(parts, summary)
	}
	writtenExpertise := 0
	for _, item := range expertise {
		if writtenExpertise >= constants.ChannelRosterMaximumExpertiseCount {
			break
		}
		title := rosterText(item.Title, constants.ChannelRosterMaximumExpertiseTitleRunes, "")
		description := rosterText(
			item.Description,
			constants.ChannelRosterMaximumExpertiseDescriptionRunes,
			"",
		)
		if title == "" && description == "" {
			continue
		}
		if title == "" {
			title = "Untitled"
		}
		if description != "" {
			parts = append(parts, title+": "+description)
		} else {
			parts = append(parts, title)
		}
		writtenExpertise++
	}
	return strings.Join(parts, "; ")
}

// teamWorkspaceSection 生成共享执行工作区说明；路径为空时不注入。
func teamWorkspaceSection(workspaceDirectory string) string {
	path := strings.TrimSpace(workspaceDirectory)
	if path == "" {
		return ""
	}
	return "## Team Workspace\nYour working directory `" + path +
		"` is the shared execution workspace for this team Chat; project-related reads and writes are visible to the lead and members."
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
