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
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	builtinskills "github.com/JieWaZi/NeuxBot/server/internal/skills/builtin"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// agentSessionSlot 对应一个真实原生会话与其独占连接文件目录。
type agentSessionSlot struct {
	// sessionID 是可靠的原生会话身份。
	sessionID string
	// directory 是该会话进程环境中保持稳定的目录。
	directory string
}

// agentSessionFiles 串行保护原生会话的连接文件映射，不保存业务授权。
type agentSessionFiles struct {
	// mutex 保护会话槽位和文件准备。
	mutex sync.Mutex
	// slots 按 Chat、Runtime 和账号保存当前真实会话。
	slots map[string]agentSessionSlot
}

func agentIdentity(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func (m *service) prepareAgent(request *runtimes.RunRequest, chat models.Chat, executionID string, capabilities []ToolCapability, allowedDomains *[]string, facts *executionMessageFacts, automaticRunIDs ...string) (func(), func(), func(runtimes.Result), error) {
	if m.agent == nil {
		return nil, nil, nil, nil
	}
	binary, err := filepath.EvalSymlinks(m.agentBinary)
	if err != nil || !filepath.IsAbs(binary) {
		return nil, nil, nil, status_error.AgentConnectionUnavailable
	}
	if info, err := os.Stat(binary); err != nil || !info.Mode().IsRegular() {
		return nil, nil, nil, status_error.AgentConnectionUnavailable
	}
	// 系统技能与执行授权描述保留在程序数据目录，不随业务工作文件切换根目录。
	root, err := filepath.EvalSymlinks(m.skillDataDirectory)
	if err != nil {
		return nil, nil, nil, status_error.AgentContextStorageError
	}
	skill, err := builtinskills.SystemDirectory(root, binary)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, item := range request.Skills {
		if item.Name == constants.AgentCLISkillName {
			return nil, nil, nil, status_error.SkillContentInvalid
		}
	}
	request.Skills = append(request.Skills, runtimes.Skill{Name: constants.AgentCLISkillName, Path: skill})
	binary = builtinskills.CLIPath(skill)
	roots := []string{}
	for _, candidate := range append([]string{request.Workspace.Path}, chat.AdditionalWorkingDirectories...) {
		if actual, err := filepath.EvalSymlinks(candidate); err == nil {
			roots = append(roots, actual)
		}
	}
	automaticRunID := ""
	if len(automaticRunIDs) > 0 {
		automaticRunID = automaticRunIDs[0]
	}
	bindings := []agentcapability.Binding{}
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
		bindings = append(bindings, m.questionBindings(chat.ID, executionID, facts)...)
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
	baseRuntimeID := request.Detection.Runtime.BaseRuntimeID
	if baseRuntimeID == "" {
		baseRuntimeID = request.Target.RuntimeID
	}
	supportsStdio, _ := runtimes.ConnectorCompatibility(baseRuntimeID, runtimes.ConnectorTransportStdio)
	useMCP := hasWorkflowCapabilities && supportsStdio
	discovery := fmt.Sprintf("Its executable is %q; invoke this exact quoted path with capabilities to discover the current authorized commands.", binary)
	if useMCP {
		discovery = "Use the injected ally_capabilities MCP tools for NeuxBot operations in this execution. Their schemas define the current inputs; use capabilities or guide only when discovery or relevant guidance is missing. Do not run CLI discovery or switch to CLI after a tool error."
	}
	request.Instructions = append(request.Instructions, runtimes.Instruction{Name: "软件能力", Content: fmt.Sprintf("For NeuxBot-provided operations, read the ally skill at %q. %s Resolve all skill-relative paths from the directory containing SKILL.md, never from the working directory. For knowledge tasks, read %q. These paths are supplied directly; do not search the workspace or global installation directories. Read only the relevant guide and live operation contract.", filepath.Join(skill, "SKILL.md"), discovery, filepath.Join(skill, "references", "wiki-guide.md"))})
	// 工具绑定是确定的场景事实，不让模型从用户短句猜测当前入口。
	for _, binding := range bindings {
		if binding.Operation == "workflow.source.save" {
			appendCurrentWorkflowReferences(request)
			request.Message.Text += "\n\n<workflow-authoring-context>This is the bound workflow authoring conversation. Short edits, corrections, and follow-up instructions still target this workflow. Use workflow.authoring.prepare or workflow.source.get through this execution's capability transport to obtain the bound workflow and current revision; no workflow ID is needed for that read. Implement requested changes through workflow.source.save and its actual saved receipt. A chat description or local file alone does not update the canvas. Pure questions may be answered without a save; do not invent edits. Use existing Wakers from resources. Ask missing consequential choices through the structured question tool; they are answered on the canvas. Never report saving or checks as successful without their receipts. Read original run evidence before optimizing a referenced failure.</workflow-authoring-context>"
			break
		}
	}
	grant, activate, revoke, err := m.agent.Prepare(agentcapability.GrantSpec{ExecutionID: executionID, Bindings: bindings, FileRoots: roots, Record: func(ctx context.Context, op contract.Operation, result agentcapability.Result) error {
		return facts.record(ctx, m.journal, chat.ID, executionID, op, result)
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
		return nil, nil, nil, err
	}
	key := agentIdentity(chat.ID + ":" + request.Target.RuntimeID + ":" + request.Target.AccountID)
	m.agentSessions.mutex.Lock()
	slot := m.agentSessions.slots[key]
	if request.Session.Mode == runtimes.SessionModeNew || slot.directory == "" || slot.sessionID != request.Session.ID {
		slot = agentSessionSlot{sessionID: request.Session.ID, directory: filepath.Join(root, "agent-contexts", key, util.New())}
	}
	path, err := agentcapability.WriteConnection(slot.directory, grant.ID+".json", agentcapability.Connection{ProtocolVersion: constants.AgentCLIProtocolVersion, Endpoint: m.agentEndpoint, CoreEpoch: grant.Epoch, GrantID: grant.ID, Token: grant.Token})
	if err == nil {
		m.agentSessions.slots[key] = slot
	}
	m.agentSessions.mutex.Unlock()
	if err != nil {
		revoke()
		return nil, nil, nil, err
	}
	request.UserInputState = m.elicitations.inputState(executionID)
	if request.Target.Environment == nil {
		request.Target.Environment = map[string]string{}
	}
	request.Target.Environment[constants.AgentCLIContextEnvironment] = path
	request.Target.Environment[constants.AgentCLIBinaryEnvironment] = binary
	// CLI 回退的本轮路径只进入新增输入；MCP 直接绑定本轮连接，不向模型暴露连接文件。
	if useMCP {
		request.Message.Text += "\n\n<neuxbot-execution-context>Use the injected ally_capabilities MCP tools for NeuxBot operations in this execution. Use the advertised tool schemas and requestId for mutations. Recover uncertain outcomes with receipts_get using the original operation and requestId; do not switch to CLI or create a replacement request. Preserve actual tool errors and follow their retryAdvice. A successful workflow_source_save receipt proves saving; a run start receipt is only acceptance.</neuxbot-execution-context>"
		request.Connectors = append(request.Connectors, runtimes.Connector{ID: "neuxbot-internal-capabilities", Name: "NeuxBot", RuntimeKey: "ally_capabilities", Transport: runtimes.ConnectorTransportStdio, Command: binary, Arguments: []string{"mcp", "serve", "--context", path}})
	} else {
		request.Message.Text += fmt.Sprintf("\n\n<neuxbot-execution-context>For all NeuxBot CLI calls in this turn use %q --context %q. This execution-scoped path supersedes prior-turn context; never edit or copy its contents. A successful source.save receipt is a saved workflow revision; a run start receipt is only acceptance.</neuxbot-execution-context>", binary, path)
	}

	inheritedPath := request.Target.Environment["PATH"]
	if inheritedPath == "" {
		inheritedPath = os.Getenv("PATH")
	}
	request.Target.Environment["PATH"] = filepath.Dir(binary) + string(filepath.ListSeparator) + inheritedPath
	settle := func(result runtimes.Result) {
		if sessionID := reliableRuntimeSessionID(result); sessionID != "" {
			m.agentSessions.mutex.Lock()
			defer m.agentSessions.mutex.Unlock()
			if current := m.agentSessions.slots[key]; current.directory == slot.directory {
				current.sessionID = sessionID
				m.agentSessions.slots[key] = current
			}
		}
	}
	return activate, func() { revoke(); m.elicitations.finishQuestions(chat.ID, executionID) }, settle, nil
}

// appendCurrentWorkflowReferences 将当前轮引用写入实际发送的消息，原生续聊即使不重载 Instructions 也能获得准确目标。
// 此处运行在用户消息接纳之后，只修改 Runtime 请求，不改写 Chat 权威用户正文。
func appendCurrentWorkflowReferences(request *runtimes.RunRequest) {
	references := []runtimes.Instruction{}
	for _, instruction := range request.Instructions {
		if instruction.Name == "WORKFLOW_CANVAS_REFERENCES.md" || instruction.Name == "WORKFLOW_RUN_REFERENCE.md" {
			references = append(references, instruction)
		}
	}
	encoded, _ := json.Marshal(references)
	request.Message.Text += "\n\n<workflow-reference-context>This is the authoritative reference context for this turn. Read the referenced revision to locate its exact stable targets before editing the current head; never infer the selected node or phase from wording alone. An empty list means this turn has no selected canvas or run references; do not silently reuse references from earlier turns. Keep internal identities out of user-facing replies.\n" + string(encoded) + "\n</workflow-reference-context>"
}
