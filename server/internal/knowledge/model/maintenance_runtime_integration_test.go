package model

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	acpdriver "github.com/JieWaZi/NeuxBot/server/internal/agent/core/driver/acp"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/host"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

// maintenanceACPAgent 只替换原生 Agent 边界，公开 ACP 传输、Driver 和执行内核均使用真实实现。
type maintenanceACPAgent struct {
	// connection 保存公开 acpserver 注入的 Agent 端连接。
	connection atomic.Pointer[sdk.AgentSideConnection]
	// bound 保证 Initialize 不越过连接注入。
	bound chan struct{}
	// newSession 核验真实 ACP 新建请求。
	newSession func(sdk.NewSessionRequest)
	// prompt 执行当前回归的工具循环与消息通知。
	prompt func(context.Context, sdk.PromptRequest) (sdk.PromptResponse, error)
	// closeNative 核验原生消费者退出前的授权及文件状态。
	closeNative func()
	// options 保存原生实际确认的模型、推理和权限目录。
	options []sdk.SessionConfigOption
	// prompts 统计真实提交，防止工具循环或清理重放任务。
	prompts atomic.Int32
	// closes 统计原生退出次数。
	closes atomic.Int32
}

// SetAgentConnection 接收公开 ACP 服务创建的唯一连接。
func (agent *maintenanceACPAgent) SetAgentConnection(connection *sdk.AgentSideConnection) {
	agent.connection.Store(connection)
	close(agent.bound)
}

// Initialize 等待连接准备并确认公开协议版本。
func (agent *maintenanceACPAgent) Initialize(ctx context.Context, _ sdk.InitializeRequest) (sdk.InitializeResponse, error) {
	select {
	case <-agent.bound:
		return sdk.InitializeResponse{ProtocolVersion: sdk.ProtocolVersionNumber}, nil
	case <-ctx.Done():
		return sdk.InitializeResponse{}, ctx.Err()
	}
}

// NewSession 确认全新工作区并提供真实可选配置。
func (agent *maintenanceACPAgent) NewSession(_ context.Context, request sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
	agent.newSession(request)
	return sdk.NewSessionResponse{SessionId: "wiki-native-session", ConfigOptions: agent.options}, nil
}

// SetSessionConfigOption 返回实际应用后的完整配置目录。
func (agent *maintenanceACPAgent) SetSessionConfigOption(_ context.Context, request sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
	for _, option := range agent.options {
		if option.Select.Id == request.ValueId.ConfigId {
			option.Select.CurrentValue = request.ValueId.Value
		}
	}
	return sdk.SetSessionConfigOptionResponse{ConfigOptions: agent.options}, nil
}

// Prompt 通过真实协议执行一次独立任务。
func (agent *maintenanceACPAgent) Prompt(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
	agent.prompts.Add(1)
	return agent.prompt(ctx, request)
}

// Close 在原生资源仍存在时核验授权撤销顺序。
func (agent *maintenanceACPAgent) Close(context.Context) error {
	agent.closes.Add(1)
	agent.closeNative()
	return nil
}

// Cancel 由公开 SDK 管理请求取消，本替身不增加第二套状态。
func (*maintenanceACPAgent) Cancel(context.Context, sdk.CancelNotification) error { return nil }

// Authenticate 拒绝独立任务没有申请的认证。
func (*maintenanceACPAgent) Authenticate(context.Context, sdk.AuthenticateRequest) (sdk.AuthenticateResponse, error) {
	return sdk.AuthenticateResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodAuthenticate)
}

// Logout 拒绝独立任务没有申请的登出。
func (*maintenanceACPAgent) Logout(context.Context, sdk.LogoutRequest) (sdk.LogoutResponse, error) {
	return sdk.LogoutResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodLogout)
}

// CloseSession 拒绝本回归未使用的单会话关闭扩展。
func (*maintenanceACPAgent) CloseSession(context.Context, sdk.CloseSessionRequest) (sdk.CloseSessionResponse, error) {
	return sdk.CloseSessionResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionClose)
}

// ListSessions 拒绝独立任务枚举其他会话。
func (*maintenanceACPAgent) ListSessions(context.Context, sdk.ListSessionsRequest) (sdk.ListSessionsResponse, error) {
	return sdk.ListSessionsResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionList)
}

// ResumeSession 拒绝恢复其他会话，维护必须使用全新会话。
func (*maintenanceACPAgent) ResumeSession(context.Context, sdk.ResumeSessionRequest) (sdk.ResumeSessionResponse, error) {
	return sdk.ResumeSessionResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionResume)
}

// SetSessionMode 拒绝绕过已声明配置目录切换权限。
func (*maintenanceACPAgent) SetSessionMode(context.Context, sdk.SetSessionModeRequest) (sdk.SetSessionModeResponse, error) {
	return sdk.SetSessionModeResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionSetMode)
}

// TestMaintenanceRuntimeIntegratesScopedACPTask 串联维护领域、共享宿主、真实内核及公开 ACP，保护授权证据和最终报告。
func TestMaintenanceRuntimeIntegratesScopedACPTask(t *testing.T) {
	for _, callKnowledge := range []bool{true, false} {
		t.Run(map[bool]string{true: "authorized-knowledge-operation", false: "discovery-cannot-prove-maintenance"}[callKnowledge], func(t *testing.T) {
			registry := agentcapability.New(nil)
			t.Cleanup(registry.Close)
			binary := filepath.Join(t.TempDir(), "allycli")
			if err := os.WriteFile(binary, []byte("bundled-test-allycli"), 0700); err != nil {
				t.Fatal(err)
			}
			var workspace, accountFile, connectionPath, skillPath string
			var connection agentcapability.Connection
			var domainCalls, accountReleases atomic.Int32
			agent := &maintenanceACPAgent{bound: make(chan struct{})}
			for _, choice := range []struct {
				category sdk.SessionConfigOptionCategory // category 是公开原生配置分类。
				id       string                          // id 是公开配置身份。
				value    string                          // value 是任务实际要求的值。
			}{{sdk.SessionConfigOptionCategoryMode, "mode", "agent-full-access"}, {sdk.SessionConfigOptionCategoryModel, "model", "wiki-model"}, {sdk.SessionConfigOptionCategoryThoughtLevel, "reasoning", "high"}} {
				choices := sdk.SessionConfigSelectOptionsUngrouped{{Value: sdk.SessionConfigValueId(choice.value), Name: choice.value}}
				category := choice.category
				agent.options = append(agent.options, sdk.SessionConfigOption{Select: &sdk.SessionConfigOptionSelect{Id: sdk.SessionConfigId(choice.id), Name: choice.id, Category: &category, CurrentValue: sdk.SessionConfigValueId(choice.value), Options: sdk.SessionConfigSelectOptions{Ungrouped: &choices}}})
			}
			agent.newSession = func(request sdk.NewSessionRequest) {
				if request.Cwd != workspace || len(request.McpServers) != 0 {
					t.Errorf("native session lost task workspace or inherited unsupported MCP: %+v", request)
				}
			}
			agent.closeNative = func() {
				if response := maintenanceCapabilities(registry, connection); response.Code != http.StatusUnauthorized {
					t.Errorf("native close began before grant revocation: status=%d", response.Code)
				}
				if accountReleases.Load() != 0 {
					t.Error("account lease released before native consumer stopped")
				}
				for _, path := range []string{accountFile, connectionPath, skillPath} {
					if _, err := os.ReadFile(path); err != nil {
						t.Errorf("live native dependency removed before close: %s: %v", path, err)
					}
				}
			}
			input := maintenanceInput()
			input.Instructions = "WIKI_FIXER_INSTRUCTIONS: inspect the existing library before repairing its pages."
			input.Message = "MAINTENANCE_TASK: maintain this library's existing Wiki."
			input.Bindings[0].Call = func(_ context.Context, request agentcapability.Request) (agentcapability.Result, error) {
				domainCalls.Add(1)
				if request.Operation != "knowledge.audit.get" || request.Params["libraryId"] != "library" {
					t.Errorf("domain scope changed: %+v", request)
				}
				return agentcapability.Result{Data: map[string]any{"checked": true}}, nil
			}
			const finalReport = "维护完成：已检查现有 Wiki。"
			agent.prompt = func(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
				if len(request.Prompt) != 1 || request.Prompt[0].Text == nil {
					t.Errorf("task did not reach the public ACP text Prompt: %+v", request.Prompt)
					return sdk.PromptResponse{}, sdk.NewInvalidParams(nil)
				}
				prompt := request.Prompt[0].Text.Text
				for _, supplied := range []string{input.Instructions, input.Message, skillPath, "Read the supplied ally skill."} {
					if strings.Count(prompt, supplied) != 1 {
						t.Errorf("supplied context must reach native Prompt exactly once: %q in %q", supplied, prompt)
					}
				}
				if strings.Contains(prompt, connection.Token) {
					t.Error("invocation credential leaked into model-visible instructions")
				}
				if response := maintenanceCapabilities(registry, connection); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "knowledge.audit.get") || strings.Contains(response.Body.String(), "im.targets.list") {
					t.Errorf("prepared invocation has the wrong authorized commands: status=%d body=%s", response.Code, response.Body)
					return sdk.PromptResponse{}, sdk.NewInternalError(nil)
				}
				id := "maintenance-commentary"
				updates := []sdk.SessionUpdate{
					{AgentMessageChunk: &sdk.SessionUpdateAgentMessageChunk{MessageId: &id, Content: sdk.TextBlock("过程说明不能进入维护报告"), Meta: map[string]any{"acp-go/message": map[string]any{"phase": "commentary", "complete": true, "source": "native_phase"}}}},
					sdk.StartToolCall("maintenance-command", "knowledge.audit.get", sdk.WithStartStatus(sdk.ToolCallStatusInProgress)),
				}
				for _, update := range updates {
					if err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: request.SessionId, Update: update}); err != nil {
						return sdk.PromptResponse{}, err
					}
				}
				if callKnowledge {
					if response := maintenanceAudit(registry, connection); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"ok":true`) || !strings.Contains(response.Body.String(), `"checked":true`) {
						t.Errorf("actual authorized knowledge command failed: status=%d body=%s", response.Code, response.Body)
						return sdk.PromptResponse{}, sdk.NewInternalError(nil)
					}
				}
				if err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: request.SessionId, Update: sdk.UpdateToolCall("maintenance-command", sdk.WithUpdateStatus(sdk.ToolCallStatusCompleted))}); err != nil {
					return sdk.PromptResponse{}, err
				}
				id = "maintenance-final"
				for index, text := range []string{"维护完成：", "已检查现有 Wiki。"} {
					update := sdk.SessionUpdate{AgentMessageChunk: &sdk.SessionUpdateAgentMessageChunk{MessageId: &id, Content: sdk.TextBlock(text), Meta: map[string]any{"acp-go/message": map[string]any{"phase": "final", "complete": index == 1, "source": "native_phase"}}}}
					if err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: request.SessionId, Update: update}); err != nil {
						return sdk.PromptResponse{}, err
					}
				}
				return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
			}
			host := runtimehost.New(runtimehost.Config{
				Resolve: func(context.Context, runtimehost.TextRequest) (runtimehost.Environment, error) {
					return runtimehost.Environment{BaseRuntimeID: runtimeconfig.RuntimeIDCodex, Target: runtimeconfig.RuntimeTarget{RuntimeID: "wiki-profile", Binary: "/selected/codex"}}, nil
				},
				Prepare: func(_ context.Context, request runtimehost.TextRequest, environment runtimehost.Environment, root string) (runtimeconfig.RuntimeTarget, func(context.Context) error, error) {
					workspace = root
					if request.RuntimeID != "wiki-profile" || request.AccountID != "wiki-account" || request.Model != "wiki-model" || request.Reasoning != "high" {
						t.Errorf("accepted model/account selection changed: %+v", request)
					}
					accountDirectory := filepath.Join(root, "account-home")
					if err := os.Mkdir(accountDirectory, 0700); err != nil {
						return runtimeconfig.RuntimeTarget{}, nil, err
					}
					accountFile = filepath.Join(accountDirectory, "account.token")
					if err := os.WriteFile(accountFile, []byte("selected-test-account"), 0600); err != nil {
						return runtimeconfig.RuntimeTarget{}, nil, err
					}
					target := environment.Target
					target.Isolated = true
					target.Environment = map[string]string{"ACCOUNT_FILE": accountFile, "PATH": "native-tools"}
					return target, func(context.Context) error {
						accountReleases.Add(1)
						return os.RemoveAll(accountDirectory)
					}, nil
				},
				Launch: func(target runtimeconfig.RuntimeTarget, _ runtimeconfig.DetectionResult) (runtimeconfig.LaunchFacts, error) {
					return runtimeconfig.LaunchFacts{BaseRuntimeID: runtimeconfig.RuntimeIDCodex, Executable: target.Binary, Environment: []string{"ACCOUNT_FILE=" + target.Environment["ACCOUNT_FILE"], "PATH=" + target.Environment["PATH"]}}, nil
				},
				Driver: func(environment runtimehost.Environment, _ acpdriver.Factory) (c.Driver, error) {
					return acpdriver.New(acpdriver.Config{ID: environment.Target.RuntimeID, CompletionPolicy: c.CompletionNativePhase, ModeCandidates: []string{"agent-full-access"}, Factory: func(_ context.Context, request c.OpenRequest) (sdk.Agent, error) {
						if request.Session.Mode != c.SessionNew || request.Policies.Tools.Mode != c.ToolsAllowAll || request.Policies.NativeConfig != c.NativeConfigInherit {
							t.Errorf("maintenance execution policy changed: %+v", request)
						}
						if request.Credential.Environment["ACCOUNT_FILE"] != accountFile || !strings.HasSuffix(request.Credential.Environment["PATH"], "native-tools") || len(request.Resources) != 1 || request.Resources[0].ID != "ally" {
							t.Errorf("task launch lost account facts or skill binding: %+v", request)
							return nil, status_error.RuntimeConfigurationInvalid
						}
						connectionPath = request.Credential.Environment[constants.AgentCLIContextEnvironment]
						var err error
						connection, err = agentcapability.ReadConnection(connectionPath)
						if err != nil {
							return nil, err
						}
						skillPath = filepath.Join(request.Resources[0].Path, "SKILL.md")
						relative, err := filepath.Rel(workspace, skillPath)
						if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
							t.Errorf("supplied skill escaped the native workspace: %s", skillPath)
						}
						if skill, err := os.ReadFile(skillPath); err != nil || !strings.Contains(string(skill), "allycli") {
							t.Errorf("supplied ally skill is unreadable or empty: %v", err)
						}
						if cli, err := os.ReadFile(request.Credential.Environment[constants.AgentCLIBinaryEnvironment]); err != nil || string(cli) != "bundled-test-allycli" {
							t.Errorf("supplied CLI source changed: %v", err)
						}
						return agent, nil
					}})
				},
			})
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := host.Close(ctx); err != nil {
					t.Error(err)
				}
			})
			runner := NewMaintenanceRunner(MaintenanceConfig{Runtime: host, Registry: registry, Endpoint: "http://127.0.0.1:8432" + constants.AgentCLIPath, Binary: binary, RuntimeID: "wiki-profile", AccountID: "wiki-account", Model: "wiki-model", Reasoning: "high"})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			report, err := runner.Run(ctx, input)
			if callKnowledge {
				if err != nil || report != finalReport || domainCalls.Load() != 1 {
					t.Fatalf("authorized maintenance report=%q err=%v domain calls=%d", report, err, domainCalls.Load())
				}
			} else {
				var coded interface{ Code() int64 }
				if report != "" || !errors.As(err, &coded) || coded.Code() != status_error.RuntimeExecutionFailed.Code() || domainCalls.Load() != 0 {
					t.Fatalf("discovery and native narrative bypassed domain proof: report=%q err=%v domain calls=%d", report, err, domainCalls.Load())
				}
			}
			if agent.prompts.Load() != 1 || agent.closes.Load() != 1 || accountReleases.Load() != 1 {
				t.Fatalf("task or cleanup replayed: prompts=%d native closes=%d account releases=%d", agent.prompts.Load(), agent.closes.Load(), accountReleases.Load())
			}
			for _, path := range []string{workspace, accountFile, connectionPath, skillPath} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Errorf("closed native task retained dependency: %s: %v", path, err)
				}
			}
		})
	}
}
