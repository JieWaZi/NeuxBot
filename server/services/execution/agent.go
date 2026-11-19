package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"

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
	// mcpOperations 是内部 MCP 公布的有序工具身份，用于原生历史兼容判断。
	mcpOperations []string
	// activate 在业务接纳后启用本轮工具范围。
	activate func()
	// finish 在执行结束或准备失败时结束本轮工具范围。
	finish func()
}

func (m *service) prepareAgent(ctx context.Context, environment models.RuntimeEnvironment, workspace agentapi.Workspace, skills *[]agentapi.Skill, instructions *[]agentapi.Instruction, connectors *[]agentapi.ConnectorDeclaration, contextInstructions *[]agentapi.Instruction, chat models.Chat, executionID string, request pendingMessage, requestReferencedFiles []string) (preparedAgentConnection, error) {
	if m.agent == nil {
		return preparedAgentConnection{}, nil
	}
	// 系统技能由 Core 初始化发布；每轮只读取已发布路径与内容摘要。
	skill := m.systemSkillDirectory
	if skill == "" {
		return preparedAgentConnection{}, status_error.AgentContextStorageError
	}
	root := filepath.Dir(filepath.Dir(skill))
	binary := builtinskills.CLIPath(skill)
	for _, item := range *skills {
		if item.Name == constants.AgentCLISkillName {
			return preparedAgentConnection{}, status_error.SkillContentInvalid
		}
	}
	*skills = append(*skills, agentapi.Skill{Name: constants.AgentCLISkillName, Path: skill, Digest: filepath.Base(skill)})
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
		bindings = append(bindings, capability.Bindings...)
	}
	onboardingBindings, err := m.prepareWakerOnboarding(ctx, chat, executionID, request, instructions, contextInstructions)
	if err != nil {
		return preparedAgentConnection{}, err
	}
	bindings = append(bindings, onboardingBindings...)
	if len(onboardingBindings) == 0 {
		bindings = append(bindings, m.wakerProfileEditBindings(chat, executionID, request)...)
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
	hasWorkflowCapabilities := false
	for _, binding := range bindings {
		if strings.HasPrefix(binding.Operation, "workflow.") {
			hasWorkflowCapabilities = true
			break
		}
	}
	baseRuntimeID := nativeProvider(environment)
	if baseRuntimeID == "" {
		baseRuntimeID = request.runtime.RuntimeID
	}
	supportsStdio, _ := agentapi.ConnectorCompatibility(baseRuntimeID, agentapi.ConnectorTransportStdio)
	useMCP := hasWorkflowCapabilities && supportsStdio
	m.appendAgentInstructions(instructions, useMCP, binary, skill)
	// 工具绑定是确定的场景事实，不让模型从用户短句猜测当前入口。
	for _, binding := range bindings {
		if binding.Operation == "workflow.source.save" {
			// 创作角色与正式指南进入稳定指令，本轮只补充当前引用。
			guide, err := builtinskills.Guide("workflow.authoring")
			if err != nil {
				return preparedAgentConnection{}, err
			}
			*instructions = append(*instructions, agentapi.Instruction{Name: "WORKFLOW_AUTHORING.md", Content: fmt.Sprintf("Apply this host-bound workflow authoring role instead of general instructions to immediately perform business requests. The required guide below is supplied from %q; resolve its relative references from %q.\n\n%s", filepath.Join(skill, "references", "workflow-authoring.md"), filepath.Join(skill, "references"), guide)})
			appendCurrentWorkflowReferences(contextInstructions)
			break
		}
	}
	// 原生 MCP 连接会保留工具结构，操作集合变化时使用独立连接。
	identity := []string{chat.ID, request.runtime.RuntimeID, request.runtime.AccountID}
	var mcpOperations []string
	if useMCP {
		mcpOperations = make([]string, 0, len(bindings))
		for _, binding := range bindings {
			mcpOperations = append(mcpOperations, binding.Operation)
		}
		slices.Sort(mcpOperations)
		identity = append(identity, mcpOperations...)
	}
	key := agentIdentity(strings.Join(identity, "\x00"))
	path, activate, finish, err := m.agent.PrepareConnection(filepath.Join(root, constants.AgentCLIContextDirectory), key, m.agentEndpoint, capability.GrantSpec{ExecutionID: executionID, Bindings: bindings, FileRoots: roots, FilePaths: requestReferencedFiles, ManagedSpace: workspace.Path, Record: func(ctx context.Context, op capability.Operation, result capability.Result) error {
		_, err := m.journal.AppendExecutionFacts(ctx, chat.ID, []eventlog.AppendInput{{Type: constants.ChatEventTypeAgentCommandCommitted,
			ExecutionID: executionID, InvocationID: executionID, Data: marshalData(eventlog.AgentCommandData{CommandID: result.CommandID, Operation: op.ID, Completion: op.Completion})}})
		return err
	}, Validate: func(ctx context.Context) error {
		current, err := m.journal.LoadExecutionChat(ctx, chat.ID)
		if err != nil {
			return err
		}
		if current.WakerID != chat.WakerID || current.ActiveExecutionID != executionID {
			return status_error.AgentContextUnauthorized
		}
		return nil
	}})
	if err != nil {
		return preparedAgentConnection{}, err
	}
	// 两种传输都由宿主绑定连接文件，模型只消费稳定工具说明。
	if useMCP {
		*connectors = append(*connectors, agentapi.ConnectorDeclaration{ID: "neuxbot-internal-capabilities", Name: "NeuxBot", RuntimeKey: "bot_capabilities", Transport: agentapi.ConnectorTransportStdio, Command: binary, Arguments: []string{"mcp", "serve", "--context", path}})
	}

	prepared := preparedAgentConnection{mcpOperations: mcpOperations, activate: activate, finish: finish}
	if !useMCP {
		prepared.cliContextFile = path
	}
	return prepared, nil
}

// appendAgentInstructions 提供稳定的工具说明，连接由宿主交付。
func (m *service) appendAgentInstructions(instructions *[]agentapi.Instruction, useMCP bool, binary, skill string) {
	discovery := fmt.Sprintf("Its executable is %q; invoke this exact quoted path with capabilities to discover the current authorized commands. The host provides the active connection.", binary)
	if useMCP {
		discovery = "Use the injected bot_capabilities MCP tools for NeuxBot operations in this execution. Their schemas define the current inputs; use capabilities or guide only when discovery or relevant guidance is missing. Use requestId for mutations. Recover uncertain outcomes with receipts_get using the original operation and requestId; do not create a replacement request. Preserve actual tool errors and follow their retryAdvice. Do not run CLI discovery or switch to CLI after a tool error."
	}
	*instructions = append(*instructions, agentapi.Instruction{Name: "NeuxBot capabilities", Content: fmt.Sprintf("For NeuxBot-provided operations, read the bot skill at %q. %s Resolve all skill-relative paths from the directory containing SKILL.md, never from the working directory. For knowledge tasks, read %q. These paths are supplied directly; do not search the workspace or global installation directories. Read only the relevant guide and live operation contract. A successful workflow.source.save receipt proves saving; a run start receipt is only acceptance.", filepath.Join(skill, "SKILL.md"), discovery, filepath.Join(skill, "references", "wiki-guide.md"))})
}

// appendCurrentWorkflowReferences 归并本轮引用，由统一上下文投影发送一次。
// 空列表明确清除前轮选择；伙伴和团队等稳定指令不参与本轮引用。
func appendCurrentWorkflowReferences(contextInstructions *[]agentapi.Instruction) {
	references := []agentapi.Instruction{}
	context := make([]agentapi.Instruction, 0, len((*contextInstructions))+1)
	for _, instruction := range *contextInstructions {
		switch instruction.Name {
		case "WORKFLOW_CANVAS_REFERENCES.md", "WORKFLOW_RUN_REFERENCE.md":
			if json.Valid([]byte(instruction.Content)) {
				references = append(references, instruction)
			}
		default:
			context = append(context, instruction)
		}
	}
	encoded, _ := json.Marshal(references)
	(*contextInstructions) = append(context, agentapi.Instruction{Name: "Current workflow references", Content: "This is the authoritative reference context for this turn. For canvas selections, read the referenced revision to locate the exact stable targets before editing the current head. Never infer the selected node or phase from wording alone. For a run reference, read its full evidence and revision source before editing. An empty list means this turn has no selected canvas or run references; do not silently reuse references from earlier turns. Keep internal identities out of user-facing replies.\n" + string(encoded)})
}
