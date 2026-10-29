package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"
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

func (m *service) prepareAgent(request *runtimecontract.RunRequest, chat models.Chat, executionID string, capabilities []ToolCapability, allowedDomains *[]string, facts *executionMessageFacts, automaticRunIDs ...string) (func(), func(), func(runtimecontract.Result), error) {
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
	request.Skills = append(request.Skills, runtimecontract.Skill{Name: constants.AgentCLISkillName, Path: skill})
	binary = builtinskills.CLIPath(skill)
	request.Instructions = append(request.Instructions, runtimecontract.Instruction{Name: "软件能力", Content: fmt.Sprintf("For NeuxBot-provided operations, read the ally skill at %q. Its executable is %q; invoke this exact quoted path with capabilities to discover the current authorized commands. Resolve all skill-relative paths from the directory containing SKILL.md, never from the working directory. For knowledge tasks, read %q. These paths are supplied directly; do not search the workspace or global installation directories. Read only the relevant guide and live operation contract.", filepath.Join(skill, "SKILL.md"), binary, filepath.Join(skill, "references", "wiki-guide.md"))})
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
	if request.Session.Mode == runtimecontract.SessionModeNew || slot.directory == "" || slot.sessionID != request.Session.ID {
		slot = agentSessionSlot{sessionID: request.Session.ID, directory: filepath.Join(root, "agent-contexts", key, util.New())}
	}
	path, err := agentcapability.WriteConnection(slot.directory, "connection.json", agentcapability.Connection{ProtocolVersion: constants.AgentCLIProtocolVersion, Endpoint: m.agentEndpoint, CoreEpoch: grant.Epoch, GrantID: grant.ID, Token: grant.Token})
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

	inheritedPath := request.Target.Environment["PATH"]
	if inheritedPath == "" {
		inheritedPath = os.Getenv("PATH")
	}
	request.Target.Environment["PATH"] = filepath.Dir(binary) + string(filepath.ListSeparator) + inheritedPath
	settle := func(result runtimecontract.Result) {
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
