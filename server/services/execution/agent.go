package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	builtinskills "github.com/JieWaZi/NeuxBot/server/internal/skills/builtin"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func agentIdentity(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

// preparedAgentConnection 保存宿主连接路径和本轮工具范围的生命周期。
type preparedAgentConnection struct {
	// cliContextFile 是 CLI 分支交给原生进程的连接文件路径。
	cliContextFile string
	// cliExecutable 是 CLI 分支使用的已发布系统技能可执行文件。
	cliExecutable string
	// mcpOperations 是内部 MCP 公布的有序工具身份，用于原生历史兼容判断。
	mcpOperations []string
	// singleExecution 表示本连接的凭据只属于当前执行，不登记可被下一轮重绑的别名。
	singleExecution bool
	// activate 在业务接纳后启用本轮工具范围。
	activate func()
	// finish 在执行结束或准备失败时结束本轮工具范围。
	finish func()
}

func (m *service) prepareAgent(ctx, executionContext context.Context, environment models.RuntimeEnvironment, workspace agentapi.Workspace, skills *[]agentapi.Skill, instructions *[]agentapi.Instruction, connectors *[]agentapi.ConnectorDeclaration, contextInstructions *[]agentapi.Instruction, chat models.Chat, executionID string, request pendingMessage, requestReferencedFiles, uploadedFiles []string) (preparedAgentConnection, error) {
	// 调用方显式签发的能力和定向工作流属于本轮依赖。普通工作台的默认工具可以省略。
	required := request.message.Workflow != nil || request.workflowCreationSessionID != "" ||
		request.message.Actor == constants.WakerOnboardingActor || len(request.toolCapabilities) != 0 ||
		(request.workflowBinding.Capability != nil && !request.localWorkflowTools)
	if m.agent == nil {
		if required {
			return preparedAgentConnection{}, status_error.AgentConnectionUnavailable
		}
		return preparedAgentConnection{}, nil
	}
	// 系统技能由 Core 初始化发布；每轮只读取已发布路径与内容摘要。
	skill := m.systemSkillDirectory
	if !availableSystemSkill(skill) {
		if required {
			return preparedAgentConnection{}, status_error.AgentContextStorageError
		}
		return preparedAgentConnection{}, nil
	}
	root := filepath.Dir(filepath.Dir(skill))
	binary := builtinskills.CLIPath(skill)
	*skills = slices.DeleteFunc(*skills, func(item agentapi.Skill) bool { return item.Name == constants.AgentCLISkillName })
	*skills = append(*skills, agentapi.Skill{Name: constants.AgentCLISkillName, Path: skill, Digest: filepath.Base(skill)})
	instructionStart, contextStart := len(*instructions), len(*contextInstructions)
	roots := []string{}
	candidates := []string{workspace.Path}
	for _, candidate := range workspace.AdditionalWritableDirectories {
		if !sameFilesystemPath(candidate, workspace.Path) {
			candidates = append(candidates, candidate)
		}
	}
	for _, project := range workspace.ProjectDirectories {
		candidates = append(candidates, project.Path)
	}
	for _, candidate := range candidates {
		if !filepath.IsAbs(candidate) {
			continue
		}
		if actual, err := filepath.EvalSymlinks(candidate); err == nil && sameFilesystemPath(actual, candidate) && !slices.Contains(roots, actual) {
			roots = append(roots, actual)
		}
	}
	automaticRunID := request.automaticRunID
	bindings := []capability.Binding{}
	if m.agentBindings != nil && (request.allowedAgentDomains == nil || len(*request.allowedAgentDomains) > 0) {
		for _, binding := range m.agentBindings(chat, executionID, roots, automaticRunID, request.imReplyLinkID) {
			// 自动来源首发已经由业务入口签发同一整理能力；恢复时由默认绑定重建，不能重复登记。
			duplicate := false
			if automaticRunID != "" && binding.Operation == "knowledge.jobs.start" {
				for _, capability := range request.toolCapabilities {
					for _, explicit := range capability.Bindings {
						if explicit.Operation == binding.Operation {
							duplicate = true
						}
					}
				}
			}
			if duplicate {
				continue
			}
			domain, _, _ := strings.Cut(binding.Operation, ".")
			if request.allowedAgentDomains == nil || slices.Contains(*request.allowedAgentDomains, domain) {
				bindings = append(bindings, binding)
			}
		}
	}
	for _, capability := range request.toolCapabilities {
		if capability.validateWork != nil {
			if err := capability.validateWork(chat, executionID); err != nil {
				return preparedAgentConnection{}, err
			}
		}
		bindings = append(bindings, capability.Bindings...)
	}
	hasAutomaticReport := false
	if m.automaticReportBinding != nil && automaticRunID != "" &&
		(request.allowedAgentDomains == nil || slices.Contains(*request.allowedAgentDomains, "conversations")) {
		binding, err := m.automaticReportBinding(ctx, chat, executionID, automaticRunID)
		if err != nil {
			return preparedAgentConnection{}, err
		}
		if binding.Operation != "" {
			hasAutomaticReport = true
			bindings = append(bindings, binding)
		}
	}
	if m.conversationRuleBindings != nil && request.message.Actor == string(constants.ChannelMessageActorUser) && request.automaticRunID == "" &&
		(request.allowedAgentDomains == nil || slices.Contains(*request.allowedAgentDomains, "routine")) &&
		(request.entryPoint == WorkbenchEntryPoint || request.entryPoint == ConversationRuleTeamEntryPoint ||
			request.entryPoint == MainConversationEntryPoint || request.entryPoint == MainConversationSourceEntryPoint) {
		selected := WorkOptions{
			RuntimeID: request.runtime.RuntimeID, AccountID: request.runtime.AccountID,
			Model: request.runtime.Model, Reasoning: request.runtime.Reasoning,
			PermissionLevel: request.runtime.PermissionLevel, WorkspaceReadOnly: request.runtime.WorkspaceReadOnly,
		}
		bindings = append(bindings, m.conversationRuleBindings(ctx, chat, executionID, selected, request.entryPoint)...)
	}
	if workflow := request.workflowBinding.Capability; workflow != nil {
		bindings = append(bindings, workflow.Bindings...)
		if guidance := request.workflowBinding.DeveloperInstruction; guidance != "" {
			*instructions = append(*instructions, agentapi.Instruction{Name: "Workflow tools", Content: guidance})
		}
	}
	if request.entryPoint == WorkbenchEntryPoint && m.resourceManagementBindings != nil {
		approve := m.resourceApproval(chat.ID, executionID)
		for _, binding := range m.resourceManagementBindings(chat, executionID, approve) {
			domain, _, _ := strings.Cut(binding.Operation, ".")
			if request.allowedAgentDomains == nil || slices.Contains(*request.allowedAgentDomains, domain) {
				bindings = append(bindings, binding)
			}
		}
	}
	onboardingBindings, err := m.prepareWakerOnboarding(ctx, chat, executionID, request, instructions, contextInstructions)
	if err != nil {
		return preparedAgentConnection{}, err
	}
	bindings = append(bindings, onboardingBindings...)
	if len(onboardingBindings) == 0 {
		profileEditBindings := m.wakerProfileEditBindings(chat, executionID, request)
		bindings = append(bindings, profileEditBindings...)
		if len(profileEditBindings) != 0 {
			*instructions = append(*instructions, agentapi.Instruction{Name: "Profile editor rules", Content: agentapi.ProfileEditInstructions})
		}
	}
	if request.allowedAgentDomains == nil || slices.Contains(*request.allowedAgentDomains, "interaction") {
		for _, question := range m.questionBindings(chat.ID, executionID) {
			// 建档使用专用选项卡；保留同一问答读取与等待能力。
			if len(onboardingBindings) > 0 && question.Operation == "interaction.questions.create" {
				continue
			}
			bindings = append(bindings, question)
		}
	}
	var hasWorkflowCapabilities, hasSkillManagement, hasConnectorManagement, hasTicketReport, hasTicketDelegation, hasRoutineRules bool
	for _, binding := range bindings {
		if strings.HasPrefix(binding.Operation, "workflow.") {
			hasWorkflowCapabilities = true
		}
		switch binding.Operation {
		case capability.RuleCreate:
			hasRoutineRules = true
		case "skills.packages.revise":
			hasSkillManagement = true
		case "mcp.connectors.update":
			hasConnectorManagement = true
		case constants.WorkTicketReportOperation:
			hasTicketReport = true
		case constants.WorkTicketDelegateOperation:
			hasTicketDelegation = true
		}
		if binding.Operation == "workflow.runs.start" && len(uploadedFiles) != 0 {
			// 仅授权本轮已接纳的附件路径，附件父目录继续隔离。
			requestReferencedFiles = append(slices.Clone(requestReferencedFiles), uploadedFiles...)
		}
	}
	baseRuntimeID := nativeProvider(environment)
	if baseRuntimeID == "" {
		baseRuntimeID = request.runtime.RuntimeID
	}
	supportsStdio, _ := agentapi.ConnectorCompatibility(baseRuntimeID, agentapi.ConnectorTransportStdio)
	publicGuidance, hasPublicPost := mainConversationInstruction(bindings)
	useMCP := (hasWorkflowCapabilities || hasPublicPost || hasTicketReport || hasRoutineRules) && supportsStdio
	*instructions = append(*instructions, agentapi.CapabilitiesInstructions(useMCP, binary, skill))
	if hasAutomaticReport {
		*instructions = append(*instructions, agentapi.Instruction{
			Name: "Automatic task delivery",
			Content: "Use conversations.messages.post (conversations_messages_post over MCP) for the task's one report to its configured destination. " +
				"Follow the task's reporting conditions. Do not quote the task input. Retry uncertain delivery with the same requestId and content.",
		})
	} else if hasPublicPost {
		*instructions = append(*instructions, publicGuidance)
	}
	if hasRoutineRules {
		*instructions = append(*instructions, agentapi.Instruction{Name: "Automatic task rules", Content: "Use routine.rules for requested schedules. Read the routine.rules guide for recurring checks. For a complex subscription, reuse or create and save a workflow, then schedule its confirmed revision. Set changes_only when the user wants alerts only for changes. When confirmation is automatic, the app shows the saved schedule. Otherwise include the confirmed task href. Saving a schedule must not start a run."})
	}
	if hasTicketReport {
		*instructions = append(*instructions, workTicketReportInstruction(useMCP, hasPublicPost))
	}
	if hasTicketDelegation {
		*instructions = append(*instructions, agentapi.Instruction{Name: "Delegated work", Content: "When a local partner or team is explicitly requested, delegate using work.tickets.delegate (work_tickets_delegate over MCP) with its known subject identity and the relevant text. " +
			"Use work.tickets.results with a report identity to read the complete report from a direct child. When required results are pending, end this turn. The host will resume you for a child question or when required results are ready. " +
			"For a waiting work report you can answer, call work.tickets.reply (work_tickets_reply over MCP) with the report identity and answer. " +
			"Use the evidence from the direct results when completing your original ticket."})
	}
	if guidance, ok := agentapi.ResourceManagementInstructions(hasSkillManagement, hasConnectorManagement); ok {
		*instructions = append(*instructions, guidance)
	}
	// 工具绑定是确定的场景事实，不让模型从用户短句猜测当前入口。
	for _, binding := range bindings {
		if binding.Operation == "workflow.source.save" && (!request.localWorkflowTools || request.workflowCreationSessionID != "") {
			// 创作角色进入会话指令，本轮只补充当前引用。
			*instructions = append(*instructions,
				agentapi.Instruction{Name: "WORKFLOW_AUTHORING.md", Content: fmt.Sprintf(agentapi.WorkflowAuthoringInstructions, filepath.Join(skill, "references", "workflow-authoring.md"))},
				agentapi.Instruction{Name: "Workflow reference rules", Content: agentapi.WorkflowReferenceInstructions},
			)
			appendCurrentWorkflowReferences(contextInstructions)
			break
		}
	}
	// 原生 MCP 连接会保留工具结构，操作集合变化时使用独立连接。
	identity := []string{chat.ID, request.runtime.RuntimeID, request.runtime.AccountID}
	if request.localWorkflowTools {
		identity = append(identity, "local-workflow")
	}
	var mcpOperations []string
	if useMCP {
		mcpOperations = make([]string, 0, len(bindings))
		for _, binding := range bindings {
			mcpOperations = append(mcpOperations, binding.Operation)
		}
		slices.Sort(mcpOperations)
		identity = append(identity, mcpOperations...)
	}
	if hasTicketReport {
		identity = append(identity, executionID)
	}
	key := agentIdentity(strings.Join(identity, "\x00"))
	path, activate, finish, err := m.agent.PrepareConnection(filepath.Join(root, constants.AgentCLIContextDirectory), key, m.agentEndpoint, capability.GrantSpec{Lifecycle: executionContext, ExecutionID: executionID, SharedLocalToken: request.localWorkflowTools, SingleExecution: hasTicketReport, Bindings: bindings, FileRoots: roots, FilePaths: requestReferencedFiles, ManagedSpace: workspace.Path, Record: func(ctx context.Context, op capability.Operation, result capability.Result) error {
		fact := eventlog.AgentCommandData{CommandID: result.CommandID, Operation: op.ID, Completion: op.Completion}
		_, err := m.journal.AppendExecutionFacts(ctx, chat.ID, []eventlog.AppendInput{{Type: constants.ChatEventTypeAgentCommandCommitted,
			ExecutionID: executionID, InvocationID: executionID, Data: marshalData(fact)}})
		return err
	}, Validate: func(context.Context) error {
		// 能力在接纳后激活，生命周期由原执行占位持有。
		if !m.IsExecutionActive(chat.ID, executionID) {
			return status_error.AgentContextUnauthorized
		}
		return nil
	}})
	if err != nil {
		if request.entryPoint == WorkbenchEntryPoint && !required && ctx.Err() == nil && errors.Is(err, status_error.AgentContextStorageError) {
			*instructions = (*instructions)[:instructionStart]
			*contextInstructions = (*contextInstructions)[:contextStart]
			*skills = (*skills)[:len(*skills)-1]
			logx.Warnf("skip unavailable local tool binding: execution_id=%s error=%v", executionID, err)
			return preparedAgentConnection{}, nil
		}
		return preparedAgentConnection{}, err
	}
	// 两种传输都由宿主绑定连接文件，模型只消费稳定工具说明。
	if useMCP {
		*connectors = append(*connectors, agentapi.ConnectorDeclaration{ID: "neuxbot-internal-capabilities", Name: "NeuxBot", RuntimeKey: "bot_capabilities", Transport: agentapi.ConnectorTransportStdio, Command: binary, Arguments: []string{"mcp", "serve", "--context", path}})
	}

	prepared := preparedAgentConnection{mcpOperations: mcpOperations, singleExecution: hasTicketReport, activate: activate, finish: finish}
	// 工单的进程随本轮连接重新装配，原生会话的兼容字段仍由原工具集合决定。
	if !useMCP || hasTicketReport {
		prepared.cliContextFile = path
		prepared.cliExecutable = binary
	}
	return prepared, nil
}

// availableSystemSkill 确认已发布的系统技能说明与二进制可用。
func availableSystemSkill(directory string) bool {
	if directory == "" {
		return false
	}
	for _, path := range []string{filepath.Join(directory, "SKILL.md"), builtinskills.CLIPath(directory)} {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
	}
	return true
}

// appendCurrentWorkflowReferences 归并本轮引用，由统一上下文投影发送一次。
// 本轮没有引用时省略字段，其余本轮事实继续发送。
func appendCurrentWorkflowReferences(contextInstructions *[]agentapi.Instruction) {
	var references []agentapi.TurnFact
	context := make([]agentapi.Instruction, 0, len((*contextInstructions))+1)
	for _, instruction := range *contextInstructions {
		switch instruction.Name {
		case "WORKFLOW_CANVAS_REFERENCES.md", "WORKFLOW_RUN_REFERENCE.md":
			if value := json.RawMessage(instruction.Content); json.Valid(value) {
				references = append(references, agentapi.TurnFact{Name: instruction.Name, Value: value})
			}
		default:
			context = append(context, instruction)
		}
	}
	if len(references) > 0 {
		encoded, _ := json.Marshal(references)
		context = append(context, agentapi.Instruction{Name: "Current workflow references", Content: string(encoded)})
	}
	*contextInstructions = context
}
