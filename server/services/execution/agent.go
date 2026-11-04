package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

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

func (m *service) prepareAgent(plan *agentapi.SessionPlan, turn *agentapi.TurnInput, chat models.Chat, executionID string, capabilities []ToolCapability, allowedDomains *[]string, automaticRunIDs ...string) (func(), func(), error) {
	appendCurrentWorkflowResults(plan, turn)
	if m.agent == nil {
		return nil, nil, nil
	}
	binary, err := filepath.EvalSymlinks(m.agentBinary)
	if err != nil || !filepath.IsAbs(binary) {
		return nil, nil, status_error.AgentConnectionUnavailable
	}
	if info, err := os.Stat(binary); err != nil || !info.Mode().IsRegular() {
		return nil, nil, status_error.AgentConnectionUnavailable
	}
	// 系统技能与执行授权描述保留在程序数据目录，不随业务工作文件切换根目录。
	root, err := filepath.EvalSymlinks(m.skillDataDirectory)
	if err != nil {
		return nil, nil, status_error.AgentContextStorageError
	}
	skill, err := builtinskills.SystemDirectory(root, binary)
	if err != nil {
		return nil, nil, err
	}
	for _, item := range plan.Skills {
		if item.Name == constants.AgentCLISkillName {
			return nil, nil, status_error.SkillContentInvalid
		}
	}
	plan.Skills = append(plan.Skills, agentapi.Skill{Name: constants.AgentCLISkillName, Path: skill})
	binary = builtinskills.CLIPath(skill)
	roots := []string{}
	for _, candidate := range append([]string{plan.Workspace.Path}, chat.AdditionalWorkingDirectories...) {
		if actual, err := filepath.EvalSymlinks(candidate); err == nil {
			roots = append(roots, actual)
		}
	}
	automaticRunID := ""
	if len(automaticRunIDs) > 0 {
		automaticRunID = automaticRunIDs[0]
	}
	bindings := []capability.Binding{}
	if m.agentBindings != nil && (allowedDomains == nil || len(*allowedDomains) > 0) {
		for _, binding := range m.agentBindings(chat, executionID, roots, automaticRunID) {
			// 自动来源首发已经由业务入口签发同一整理能力；恢复时由默认绑定重建，不能重复登记。
			duplicate := false
			if automaticRunID != "" && binding.Operation == "knowledge.jobs.start" {
				for _, capability := range capabilities {
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
			if allowedDomains == nil || slices.Contains(*allowedDomains, domain) {
				bindings = append(bindings, binding)
			}
		}
	}
	if allowedDomains == nil || slices.Contains(*allowedDomains, "interaction") {
		bindings = append(bindings, m.questionBindings(chat.ID, executionID)...)
	}
	for _, capability := range capabilities {
		bindings = append(bindings, capability.Bindings...)
	}
	hasWorkflowCapabilities := false
	for _, binding := range bindings {
		if strings.HasPrefix(binding.Operation, "workflow.") {
			hasWorkflowCapabilities = true
			break
		}
	}
	baseRuntimeID := plan.Detection.Runtime.BaseRuntimeID
	if baseRuntimeID == "" {
		baseRuntimeID = plan.Target.RuntimeID
	}
	supportsStdio, _ := agentapi.ConnectorCompatibility(baseRuntimeID, agentapi.ConnectorTransportStdio)
	useMCP := hasWorkflowCapabilities && supportsStdio
	discovery := fmt.Sprintf("Its executable is %q; invoke this exact quoted path with capabilities to discover the current authorized commands.", binary)
	if useMCP {
		discovery = "Use the injected ally_capabilities MCP tools for NeuxBot operations in this execution. Their schemas define the current inputs; use capabilities or guide only when discovery or relevant guidance is missing. Do not run CLI discovery or switch to CLI after a tool error."
	}
	plan.Instructions = append(plan.Instructions, agentapi.Instruction{Name: "软件能力", Content: fmt.Sprintf("For NeuxBot-provided operations, read the ally skill at %q. %s Resolve all skill-relative paths from the directory containing SKILL.md, never from the working directory. For knowledge tasks, read %q. These paths are supplied directly; do not search the workspace or global installation directories. Read only the relevant guide and live operation contract.", filepath.Join(skill, "SKILL.md"), discovery, filepath.Join(skill, "references", "wiki-guide.md"))})
	// 工具绑定是确定的场景事实，不让模型从用户短句猜测当前入口。
	for _, binding := range bindings {
		if binding.Operation == "workflow.source.save" {
			// 正式指南进入稳定计划；每轮提醒只补充角色，热续聊无需重复整份指南。
			guide, err := builtinskills.Guide("workflow.authoring")
			if err != nil {
				return nil, nil, err
			}
			plan.Instructions = append(plan.Instructions, agentapi.Instruction{Name: "WORKFLOW_AUTHORING.md", Content: fmt.Sprintf("Apply this host-bound workflow authoring role instead of general instructions to immediately perform business requests. The required guide below is supplied from %q; resolve its relative references from %q.\n\n%s", filepath.Join(skill, "references", "workflow-authoring.md"), filepath.Join(skill, "references"), guide)})
			appendCurrentWorkflowReferences(plan, turn)
			turn.Message.Text += "\n\n<workflow-authoring-context>Create or modify this reusable NeuxBot workflow: the first business goal and short follow-ups are authoring requirements. Do not perform the business task or substitute native CLI workflow features. Follow WORKFLOW_AUTHORING.md; start changes with workflow.authoring.prepare through this execution's selected capability transport, then workflow.source.save and its actual saved receipt. Pure explanations need no save. Resolve missing consequential choices with the structured question tool in the chat composer. Never claim saving, checks or delivery without evidence.</workflow-authoring-context>"
			break
		}
	}
	grant, activate, revoke, err := m.agent.Prepare(capability.GrantSpec{ExecutionID: executionID, Bindings: bindings, FileRoots: roots, Record: func(ctx context.Context, op capability.Operation, result capability.Result) error {
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
		return nil, nil, err
	}
	// A hot MCP bridge may keep its advertised schemas. Changes to the available
	// operation set get a new bridge path and therefore a new native connection.
	operations := make([]string, 0, len(bindings))
	if useMCP {
		for _, binding := range bindings {
			operations = append(operations, binding.Operation)
		}
		slices.Sort(operations)
	}
	key := agentIdentity(strings.Join(append([]string{chat.ID, plan.Target.RuntimeID, plan.Target.AccountID, grant.Epoch}, operations...), "\x00"))
	path, err := capability.WriteConnection(filepath.Join(root, "agent-contexts", key), "connection.json",
		capability.Connection{ProtocolVersion: constants.AgentCLIProtocolVersion, Endpoint: m.agentEndpoint, CoreEpoch: grant.Epoch, GrantID: grant.ID, Token: grant.Token})
	if err != nil {
		revoke()
		return nil, nil, err
	}
	turn.LaunchEnvironment = map[string]string{
		constants.AgentCLIContextEnvironment: path,
		constants.AgentCLIBinaryEnvironment:  binary,
	}
	// CLI 回退的本轮路径只进入新增输入；MCP 直接绑定本轮连接，不向模型暴露连接文件。
	if useMCP {
		turn.Message.Text += "\n\n<neuxbot-execution-context>Use the injected ally_capabilities MCP tools for NeuxBot operations in this execution. Use the advertised tool schemas and requestId for mutations. Recover uncertain outcomes with receipts_get using the original operation and requestId; do not switch to CLI or create a replacement request. Preserve actual tool errors and follow their retryAdvice. A successful workflow_source_save receipt proves saving; a run start receipt is only acceptance.</neuxbot-execution-context>"
		plan.Connectors = append(plan.Connectors, agentapi.ConnectorDeclaration{ID: "neuxbot-internal-capabilities", Name: "NeuxBot", RuntimeKey: "ally_capabilities", Transport: agentapi.ConnectorTransportStdio, Command: binary, Arguments: []string{"mcp", "serve", "--context", path}})
	} else {
		turn.Message.Text += fmt.Sprintf("\n\n<neuxbot-execution-context>For all NeuxBot CLI calls in this turn use %q --context %q. This execution-scoped path supersedes prior-turn context; never edit or copy its contents. A successful source.save receipt is a saved workflow revision; a run start receipt is only acceptance.</neuxbot-execution-context>", binary, path)
	}

	var once sync.Once
	return activate, func() {
		once.Do(func() {
			revoke()
			_ = os.Remove(path)
		})
	}, nil
}

// appendCurrentWorkflowResults 将本次节点结果协议放入模型输入，避免原生指令文件的加载预算截断契约。
// 只投影宿主固定命名的执行说明，不改写接纳请求中的业务消息与权威 Chat 正文。
func appendCurrentWorkflowResults(plan *agentapi.SessionPlan, turn *agentapi.TurnInput) {
	if !slices.ContainsFunc(plan.Instructions, func(instruction agentapi.Instruction) bool {
		return instruction.Name == constants.WorkflowResultsInstructionFileName
	}) {
		return
	}
	stable := make([]agentapi.Instruction, 0, len(plan.Instructions))
	for _, instruction := range plan.Instructions {
		if instruction.Name == constants.WorkflowResultsInstructionFileName {
			turn.Message.Text += "\n\n<workflow-results-context>\n" + instruction.Content + "\n</workflow-results-context>"
		} else {
			stable = append(stable, instruction)
		}
	}
	plan.Instructions = stable
}

// appendCurrentWorkflowReferences 将当前轮引用写入实际发送的消息，原生续聊即使不重载 Instructions 也能获得准确目标。
// 此处运行在用户消息接纳之后，只修改 Runtime 请求，不改写 Chat 权威用户正文。
func appendCurrentWorkflowReferences(plan *agentapi.SessionPlan, turn *agentapi.TurnInput) {
	references := []agentapi.Instruction{}
	stable := make([]agentapi.Instruction, 0, len(plan.Instructions))
	for _, instruction := range plan.Instructions {
		if instruction.Name == "WORKFLOW_CANVAS_REFERENCES.md" || instruction.Name == "WORKFLOW_RUN_REFERENCE.md" {
			references = append(references, instruction)
		} else {
			stable = append(stable, instruction)
		}
	}
	plan.Instructions = stable
	encoded, _ := json.Marshal(references)
	turn.Message.Text += "\n\n<workflow-reference-context>This is the authoritative reference context for this turn. Read the referenced revision to locate its exact stable targets before editing the current head; never infer the selected node or phase from wording alone. An empty list means this turn has no selected canvas or run references; do not silently reuse references from earlier turns. Keep internal identities out of user-facing replies.\n" + string(encoded) + "\n</workflow-reference-context>"
}
