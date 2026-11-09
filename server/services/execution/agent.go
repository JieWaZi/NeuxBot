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

func (m *service) prepareAgent(environment models.RuntimeEnvironment, workspace agentapi.Workspace, skills *[]agentapi.Skill, instructions *[]agentapi.Instruction, connectors *[]agentapi.ConnectorDeclaration, contextInstructions *[]agentapi.Instruction, chat models.Chat, executionID string, request pendingMessage, requestReferencedFiles []string) (func(), func(), error) {
	if m.agent == nil {
		return nil, nil, nil
	}
	// 系统技能由 Core 初始化发布；每轮只读取已发布路径与内容摘要。
	skill := m.systemSkillDirectory
	if skill == "" {
		return nil, nil, status_error.AgentContextStorageError
	}
	root := filepath.Dir(filepath.Dir(skill))
	binary := builtinskills.CLIPath(skill)
	for _, item := range *skills {
		if item.Name == constants.AgentCLISkillName {
			return nil, nil, status_error.SkillContentInvalid
		}
	}
	*skills = append(*skills, agentapi.Skill{Name: constants.AgentCLISkillName, Path: skill, Digest: filepath.Base(skill)})
	roots := []string{}
	candidates := []string{filepath.Join(workspace.Path, constants.TaskOutputsDirectoryName, executionID)}
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
		for _, binding := range m.agentBindings(chat, executionID, roots, automaticRunID) {
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
	if request.allowedAgentDomains == nil || slices.Contains(*request.allowedAgentDomains, "interaction") {
		bindings = append(bindings, m.questionBindings(chat.ID, executionID)...)
	}
	for _, capability := range request.toolCapabilities {
		bindings = append(bindings, capability.Bindings...)
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
				return nil, nil, err
			}
			*instructions = append(*instructions, agentapi.Instruction{Name: "WORKFLOW_AUTHORING.md", Content: fmt.Sprintf("Apply this host-bound workflow authoring role instead of general instructions to immediately perform business requests. The required guide below is supplied from %q; resolve its relative references from %q.\n\n%s", filepath.Join(skill, "references", "workflow-authoring.md"), filepath.Join(skill, "references"), guide)})
			appendCurrentWorkflowReferences(contextInstructions)
			break
		}
	}
	grant, activate, revoke, err := m.agent.Prepare(capability.GrantSpec{ExecutionID: executionID, Bindings: bindings, FileRoots: roots, FilePaths: requestReferencedFiles, Record: func(ctx context.Context, op capability.Operation, result capability.Result) error {
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
	key := agentIdentity(strings.Join(append([]string{chat.ID, request.runtime.RuntimeID, request.runtime.AccountID, grant.Epoch}, operations...), "\x00"))
	path, err := capability.WriteConnection(filepath.Join(root, "agent-contexts", key), "connection.json",
		capability.Connection{ProtocolVersion: constants.AgentCLIProtocolVersion, Endpoint: m.agentEndpoint, CoreEpoch: grant.Epoch, GrantID: grant.ID, Token: grant.Token})
	if err != nil {
		revoke()
		return nil, nil, err
	}
	// CLI 回退的本轮路径只进入新增输入；MCP 直接绑定本轮连接，不向模型暴露连接文件。
	if useMCP {
		*connectors = append(*connectors, agentapi.ConnectorDeclaration{ID: "neuxbot-internal-capabilities", Name: "NeuxBot", RuntimeKey: "ally_capabilities", Transport: agentapi.ConnectorTransportStdio, Command: binary, Arguments: []string{"mcp", "serve", "--context", path}})
	} else {
		*contextInstructions = append(*contextInstructions, agentapi.Instruction{Name: "NeuxBot execution context", Content: fmt.Sprintf("For all NeuxBot CLI calls in this turn use %q --context %q. This execution-scoped path supersedes prior-turn context; never edit or copy its contents.", binary, path)})
	}

	var once sync.Once
	return activate, func() {
		once.Do(func() {
			revoke()
			_ = os.Remove(path)
		})
	}, nil
}

// appendAgentInstructions supplies the same stable capability guide without issuing a turn grant.
func (m *service) appendAgentInstructions(instructions *[]agentapi.Instruction, useMCP bool, binary, skill string) {
	discovery := fmt.Sprintf("Its executable is %q; invoke this exact quoted path with capabilities to discover the current authorized commands.", binary)
	if useMCP {
		discovery = "Use the injected ally_capabilities MCP tools for NeuxBot operations in this execution. Their schemas define the current inputs; use capabilities or guide only when discovery or relevant guidance is missing. Use requestId for mutations. Recover uncertain outcomes with receipts_get using the original operation and requestId; do not create a replacement request. Preserve actual tool errors and follow their retryAdvice. Do not run CLI discovery or switch to CLI after a tool error."
	}
	*instructions = append(*instructions, agentapi.Instruction{Name: "软件能力", Content: fmt.Sprintf("For NeuxBot-provided operations, read the ally skill at %q. %s Resolve all skill-relative paths from the directory containing SKILL.md, never from the working directory. For knowledge tasks, read %q. These paths are supplied directly; do not search the workspace or global installation directories. Read only the relevant guide and live operation contract. A successful workflow.source.save receipt proves saving; a run start receipt is only acceptance.", filepath.Join(skill, "SKILL.md"), discovery, filepath.Join(skill, "references", "wiki-guide.md"))})
}

// appendCurrentWorkflowReferences 归并本轮引用，由统一上下文投影发送一次。
// 空列表明确清除前轮选择；伙伴和团队等稳定指令不参与本轮引用。
func appendCurrentWorkflowReferences(contextInstructions *[]agentapi.Instruction) {
	references := []agentapi.Instruction{}
	context := make([]agentapi.Instruction, 0, len((*contextInstructions))+1)
	for _, instruction := range *contextInstructions {
		if instruction.Name == "WORKFLOW_CANVAS_REFERENCES.md" || instruction.Name == "WORKFLOW_RUN_REFERENCE.md" {
			references = append(references, instruction)
		} else {
			context = append(context, instruction)
		}
	}
	encoded, _ := json.Marshal(references)
	(*contextInstructions) = append(context, agentapi.Instruction{Name: "Current workflow references", Content: "This is the authoritative reference context for this turn. Read the referenced revision to locate its exact stable targets before editing the current head; never infer the selected node or phase from wording alone. An empty list means this turn has no selected canvas or run references; do not silently reuse references from earlier turns. Keep internal identities out of user-facing replies.\n" + string(encoded)})
}
