package acp_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	driver "github.com/JieWaZi/NeuxBot/agent-runtime/driver/acp"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	sdk "github.com/JieWaZi/acp-go/sdk"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

// managementAgent 为真实管道连接提供可观察的管理操作。
type managementAgent struct {
	*fixtureAgent                                                                                                // fixtureAgent 复用基础公开协议测试实现。
	fork          func(context.Context, sdk.UnstableForkSessionRequest) (sdk.UnstableForkSessionResponse, error) // fork 控制分叉结果。
	closeSession  func(context.Context, sdk.CloseSessionRequest) (sdk.CloseSessionResponse, error)               // closeSession 控制临时句柄关闭。
}

// UnstableForkSession 接收真实协议请求。
func (a *managementAgent) UnstableForkSession(ctx context.Context, r sdk.UnstableForkSessionRequest) (sdk.UnstableForkSessionResponse, error) {
	return a.fork(ctx, r)
}

// CloseSession 接收临时会话关闭请求。
func (a *managementAgent) CloseSession(ctx context.Context, r sdk.CloseSessionRequest) (sdk.CloseSessionResponse, error) {
	if a.closeSession != nil {
		return a.closeSession(ctx, r)
	}
	return sdk.CloseSessionResponse{}, nil
}
func openManagement(t *testing.T, a sdk.Agent) *driver.Management {
	t.Helper()
	m, err := driver.OpenManagement(context.Background(), func(context.Context) (sdk.Agent, error) { return a, nil }, driver.ManagementConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := m.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return m
}

// TestManagementPreparationRetainsOwnerAndRetriesClose 防止部分准备失败丢失原生资源和关闭重试能力。
func TestManagementPreparationRetainsOwnerAndRetriesClose(t *testing.T) {
	for _, stage := range []string{"factory", "initialize", "cancel"} {
		t.Run(stage, func(t *testing.T) {
			a := newFixtureAgent()
			cause := errors.New("prepare failed")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			a.closeNative = func(context.Context) error {
				if a.closes.Load() == 1 {
					return errors.New("still live")
				}
				return nil
			}
			a.initialize = func(context.Context, sdk.InitializeRequest) (sdk.InitializeResponse, error) {
				return sdk.InitializeResponse{}, cause
			}
			m, err := driver.OpenManagement(ctx, func(context.Context) (sdk.Agent, error) {
				if stage == "factory" {
					return a, cause
				}
				if stage == "cancel" {
					cancel()
				}
				return a, nil
			}, driver.ManagementConfig{})
			if m == nil || err == nil {
				t.Fatalf("owner=%v err=%v", m, err)
			}
			if err = m.Close(context.Background()); err == nil {
				t.Fatal("close failure hidden")
			}
			if err = m.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err = m.Close(context.Background()); err != nil || a.closes.Load() != 2 {
				t.Fatalf("close=%v count=%d", err, a.closes.Load())
			}
		})
	}
}

// TestManagementProbePreservesFactsAndConfirmsModel 防止跳过 setter、丢弃部分事实或发送 Prompt。
func TestManagementProbePreservesFactsAndConfirmsModel(t *testing.T) {
	for _, stage := range []string{"new_session", "setter", "mismatch", "success"} {
		t.Run(stage, func(t *testing.T) {
			a := &managementAgent{fixtureAgent: newFixtureAgent()}
			var prompts, setters, closes atomic.Int32
			a.initialize = func(context.Context, sdk.InitializeRequest) (sdk.InitializeResponse, error) {
				return sdk.InitializeResponse{ProtocolVersion: sdk.ProtocolVersionNumber, AgentInfo: &sdk.Implementation{Name: "fixture", Version: "1"}}, nil
			}
			options := []sdk.SessionConfigOption{selectionOption(sdk.SessionConfigOptionCategoryModel, "model", "target", "target")}
			a.newSession = func(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
				if stage == "new_session" {
					return sdk.NewSessionResponse{}, sdk.NewInternalError(nil)
				}
				return sdk.NewSessionResponse{SessionId: "probe", ConfigOptions: options}, nil
			}
			a.configure = func(context.Context, sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
				setters.Add(1)
				if stage == "setter" {
					return sdk.SetSessionConfigOptionResponse{}, sdk.NewInternalError(nil)
				}
				if stage == "mismatch" {
					return sdk.SetSessionConfigOptionResponse{}, nil
				}
				return sdk.SetSessionConfigOptionResponse{ConfigOptions: options}, nil
			}
			a.prompt = func(context.Context, sdk.PromptRequest) (sdk.PromptResponse, error) {
				prompts.Add(1)
				return sdk.PromptResponse{}, nil
			}
			a.closeSession = func(context.Context, sdk.CloseSessionRequest) (sdk.CloseSessionResponse, error) {
				closes.Add(1)
				return sdk.CloseSessionResponse{}, nil
			}
			got, err := openManagement(t, a).Probe(context.Background(), driver.ProbeRequest{Workspace: contract.WorkspaceScope{Root: t.TempDir()}, Model: "target", ModelConfigID: "model"})
			if (err == nil) != (stage == "success") {
				t.Fatalf("err=%v", err)
			}
			if got.Initialize.AgentInfo == nil || got.Initialize.AgentInfo.Name != "fixture" || got.SessionReady != (stage != "new_session") {
				t.Fatalf("lost facts: %+v", got)
			}
			want := int32(1)
			if stage == "new_session" {
				want = 0
			}
			if stage == "setter" && len(got.ConfigOptions) != 1 {
				t.Fatalf("setter failure lost initial configuration: %+v", got)
			}
			if prompts.Load() != 0 || setters.Load() != want || closes.Load() != want {
				t.Fatalf("calls prompt=%d setter=%d close=%d", prompts.Load(), setters.Load(), closes.Load())
			}
		})
	}
}

// TestManagementProbeMetadataAndInspection 防止套用当前模型配置或把已知空目录误判为缺失。
func TestManagementProbeMetadataAndInspection(t *testing.T) {
	for _, inspect := range []bool{false, true} {
		t.Run(map[bool]string{false: "metadata", true: "inspect"}[inspect], func(t *testing.T) {
			a := &managementAgent{fixtureAgent: newFixtureAgent()}
			var selected []string
			current := selectionOption(sdk.SessionConfigOptionCategoryThoughtLevel, "reasoning", "medium", "medium")
			other := selectionOption(sdk.SessionConfigOptionCategoryThoughtLevel, "reasoning", "high", "high")
			model := selectionOption(sdk.SessionConfigOptionCategoryModel, "model", "first", "first", "second", "third", "empty")
			(*model.Select.Options.Ungrouped)[1].Meta = acpmeta.WithModelConfigOptions(nil, []sdk.SessionConfigOption{other})
			(*model.Select.Options.Ungrouped)[3].Meta = acpmeta.WithModelConfigOptions(nil, nil)
			options := []sdk.SessionConfigOption{model, current}
			a.newSession = func(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
				return sdk.NewSessionResponse{SessionId: "probe", ConfigOptions: options}, nil
			}
			a.configure = func(_ context.Context, r sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
				selected = append(selected, string(r.ValueId.Value))
				updated := selectionOption(sdk.SessionConfigOptionCategoryModel, "model", string(r.ValueId.Value), "first", "second", "third", "empty")
				return sdk.SetSessionConfigOptionResponse{ConfigOptions: []sdk.SessionConfigOption{updated, other}}, nil
			}
			got, err := openManagement(t, a).Probe(context.Background(), driver.ProbeRequest{Workspace: contract.WorkspaceScope{Root: t.TempDir()}, ModelConfigID: "model", InspectModelOptions: inspect})
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Models) != 4 || !managementOptionsEqual(got.Models[0].ConfigOptions, options) || !managementOptionsEqual(got.Models[1].ConfigOptions, []sdk.SessionConfigOption{other}) || got.Models[3].ConfigOptions == nil || len(got.Models[3].ConfigOptions) != 0 {
				t.Fatalf("catalog: %+v", got.Models)
			}
			if inspect {
				if !reflect.DeepEqual(selected, []string{"third"}) || len(got.Models[2].ConfigOptions) != 2 {
					t.Fatalf("models=%+v selected=%v", got.Models, selected)
				}
			} else if len(selected) != 0 || got.Models[2].ConfigOptions != nil {
				t.Fatalf("guessed unknown: %+v selected=%v", got.Models, selected)
			}
		})
	}
}

// TestManagementForkPrecisionAndMetadata 防止未经声明派发分叉或遗漏耐久对账元数据。
func TestManagementForkPrecisionAndMetadata(t *testing.T) {
	for _, mode := range []string{acpmeta.ForkUnsupported, acpmeta.ForkLatest, acpmeta.ForkTurn} {
		t.Run(mode, func(t *testing.T) {
			a := &managementAgent{fixtureAgent: newFixtureAgent()}
			var calls, closed atomic.Int32
			a.initialize = func(context.Context, sdk.InitializeRequest) (sdk.InitializeResponse, error) {
				return sdk.InitializeResponse{ProtocolVersion: sdk.ProtocolVersionNumber, Meta: acpmeta.ForkMetadata(mode), AgentCapabilities: sdk.AgentCapabilities{SessionCapabilities: sdk.SessionCapabilities{Fork: acpmeta.ForkCapability(mode), Close: &sdk.SessionCloseCapabilities{}}}}, nil
			}
			request := driver.ForkRequest{SourceSessionID: "source", Position: "opaque", SourceWorkspace: t.TempDir(), ReceiptDirectory: t.TempDir(), ReconcileOnly: true, Workspace: contract.WorkspaceScope{Root: t.TempDir()}, CloseForkSession: true}
			a.fork = func(_ context.Context, r sdk.UnstableForkSessionRequest) (sdk.UnstableForkSessionResponse, error) {
				calls.Add(1)
				if r.SessionId != "source" || r.Cwd != request.Workspace.Root || acpmeta.ForkPosition(r.Meta) != "opaque" || r.Meta["forkReceiptDirectory"] != request.ReceiptDirectory || r.Meta["sourceCwd"] != request.SourceWorkspace || r.Meta["reconcileOnly"] != true {
					t.Errorf("request=%+v", r)
				}
				return sdk.UnstableForkSessionResponse{SessionId: "child"}, nil
			}
			a.closeSession = func(_ context.Context, r sdk.CloseSessionRequest) (sdk.CloseSessionResponse, error) {
				if r.SessionId != "child" {
					t.Errorf("closed source: %s", r.SessionId)
				}
				closed.Add(1)
				return sdk.CloseSessionResponse{}, nil
			}
			got, err := openManagement(t, a).Fork(context.Background(), request)
			if mode == acpmeta.ForkTurn {
				if err != nil || got.SessionID != "child" || got.Mode != mode || calls.Load() != 1 || closed.Load() != 1 {
					t.Fatalf("fork=%+v err=%v calls=%d close=%d", got, err, calls.Load(), closed.Load())
				}
			} else {
				var f *contract.Fault
				if !errors.As(err, &f) || f.Code != contract.FaultUnsupported || calls.Load() != 0 {
					t.Fatalf("unsupported: %+v %v calls=%d", got, err, calls.Load())
				}
			}
		})
	}
}

func managementOptionsEqual(left, right []sdk.SessionConfigOption) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return string(a) == string(b)
}

// TestManagementProbeEmptyCatalogAndCloseFailure 保留没有模型目录及句柄关闭失败时已确认的会话事实。
func TestManagementProbeEmptyCatalogAndCloseFailure(t *testing.T) {
	for _, scenario := range []string{"required", "allowed", "close_failed"} {
		t.Run(scenario, func(t *testing.T) {
			a := &managementAgent{fixtureAgent: newFixtureAgent()}
			a.closeSession = func(context.Context, sdk.CloseSessionRequest) (sdk.CloseSessionResponse, error) {
				if scenario == "close_failed" {
					return sdk.CloseSessionResponse{}, sdk.NewInternalError(nil)
				}
				return sdk.CloseSessionResponse{}, nil
			}
			got, err := openManagement(t, a).Probe(context.Background(), driver.ProbeRequest{Workspace: contract.WorkspaceScope{Root: t.TempDir()}, AllowEmptyModelCatalog: scenario != "required"})
			if !got.SessionReady || (err == nil) != (scenario == "allowed") {
				t.Fatalf("result=%+v err=%v", got, err)
			}
		})
	}
}

// TestManagementCancellationStopsPreparationAndSession 防止真实取消期间丢失资源或使用已取消上下文释放临时句柄。
func TestManagementCancellationStopsPreparationAndSession(t *testing.T) {
	for _, stage := range []string{"initialize", "probe"} {
		t.Run(stage, func(t *testing.T) {
			a := &managementAgent{fixtureAgent: newFixtureAgent()}
			entered := make(chan struct{})
			var closes atomic.Int32
			if stage == "initialize" {
				a.initialize = func(ctx context.Context, _ sdk.InitializeRequest) (sdk.InitializeResponse, error) {
					close(entered)
					<-ctx.Done()
					return sdk.InitializeResponse{}, ctx.Err()
				}
			} else {
				a.configure = func(ctx context.Context, _ sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
					close(entered)
					<-ctx.Done()
					return sdk.SetSessionConfigOptionResponse{}, ctx.Err()
				}
				a.newSession = func(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
					return sdk.NewSessionResponse{SessionId: "probe", ConfigOptions: []sdk.SessionConfigOption{selectionOption(sdk.SessionConfigOptionCategoryModel, "model", "first", "first")}}, nil
				}
			}
			a.closeSession = func(ctx context.Context, _ sdk.CloseSessionRequest) (sdk.CloseSessionResponse, error) {
				if ctx.Err() != nil {
					t.Error("cleanup used cancelled context")
				}
				closes.Add(1)
				return sdk.CloseSessionResponse{}, nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var m *driver.Management
			done := make(chan error, 1)
			if stage == "initialize" {
				go func() {
					var err error
					m, err = driver.OpenManagement(ctx, func(context.Context) (sdk.Agent, error) { return a, nil }, driver.ManagementConfig{})
					done <- err
				}()
			} else {
				m = openManagement(t, a)
				go func() {
					got, err := m.Probe(ctx, driver.ProbeRequest{Workspace: contract.WorkspaceScope{Root: t.TempDir()}, Model: "first", ModelConfigID: "model"})
					if !got.SessionReady {
						t.Error("cancel lost session ready")
					}
					done <- err
				}()
			}
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("operation absent")
			}
			cancel()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("cancel ignored")
				}
			case <-time.After(time.Second):
				t.Fatal("cancel blocked")
			}
			if m == nil {
				t.Fatal("cancel lost owner")
			}
			if err := m.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			if a.closes.Load() != 1 || stage == "probe" && closes.Load() != 1 {
				t.Fatalf("native=%d session=%d", a.closes.Load(), closes.Load())
			}
		})
	}
}

// TestManagementForkCapabilityAndHandleCloseRules 验证末尾分叉、能力双重确认、原生身份与关闭规则。
func TestManagementForkCapabilityAndHandleCloseRules(t *testing.T) {
	for _, scenario := range []string{"latest", "missing_capability", "invalid_identity", "keep_handle", "close_unsupported", "close_failed"} {
		t.Run(scenario, func(t *testing.T) {
			a := &managementAgent{fixtureAgent: newFixtureAgent()}
			var calls, closes atomic.Int32
			a.initialize = func(context.Context, sdk.InitializeRequest) (sdk.InitializeResponse, error) {
				caps := sdk.SessionCapabilities{Fork: acpmeta.ForkCapability(acpmeta.ForkLatest), Close: &sdk.SessionCloseCapabilities{}}
				if scenario == "missing_capability" {
					caps.Fork = nil
				}
				if scenario == "close_unsupported" {
					caps.Close = nil
				}
				return sdk.InitializeResponse{ProtocolVersion: sdk.ProtocolVersionNumber, Meta: acpmeta.ForkMetadata(acpmeta.ForkLatest), AgentCapabilities: sdk.AgentCapabilities{SessionCapabilities: caps}}, nil
			}
			a.fork = func(_ context.Context, r sdk.UnstableForkSessionRequest) (sdk.UnstableForkSessionResponse, error) {
				calls.Add(1)
				if acpmeta.ForkPosition(r.Meta) != "" {
					t.Error("invented fork position")
				}
				id := sdk.SessionId("child")
				if scenario == "invalid_identity" {
					id = r.SessionId
				}
				return sdk.UnstableForkSessionResponse{SessionId: id}, nil
			}
			a.closeSession = func(context.Context, sdk.CloseSessionRequest) (sdk.CloseSessionResponse, error) {
				closes.Add(1)
				if scenario == "close_failed" {
					return sdk.CloseSessionResponse{}, sdk.NewInternalError(nil)
				}
				return sdk.CloseSessionResponse{}, nil
			}
			got, err := openManagement(t, a).Fork(context.Background(), driver.ForkRequest{SourceSessionID: "source", Workspace: contract.WorkspaceScope{Root: t.TempDir()}, CloseForkSession: scenario != "keep_handle"})
			switch scenario {
			case "missing_capability":
				if err == nil || calls.Load() != 0 {
					t.Fatalf("unsupported fork dispatched: %+v %v", got, err)
				}
			case "invalid_identity":
				if err == nil || got.SessionID != "" || closes.Load() != 0 {
					t.Fatalf("invalid identity: %+v %v", got, err)
				}
			default:
				if got.SessionID != "child" || got.Mode != acpmeta.ForkLatest || (err != nil) != (scenario == "close_failed") {
					t.Fatalf("fork: %+v %v", got, err)
				}
				want := int32(1)
				if scenario == "keep_handle" || scenario == "close_unsupported" {
					want = 0
				}
				if closes.Load() != want {
					t.Fatalf("close count=%d want=%d", closes.Load(), want)
				}
			}
		})
	}
}

// TestManagementForkForwardsWorkspaceAndMCP 捕获技术启动参数在稳定与实验协议结构转换时丢失。
func TestManagementForkForwardsWorkspaceAndMCP(t *testing.T) {
	a := &managementAgent{fixtureAgent: newFixtureAgent()}
	workspace := contract.WorkspaceScope{Root: t.TempDir(), Directories: []string{t.TempDir()}}
	servers := []sdk.McpServer{{Stdio: &sdk.McpServerStdio{Name: "tool", Command: "fixture", Args: []string{"arg"}, Env: []sdk.EnvVariable{{Name: "TOKEN", Value: "test-secret"}}}}}
	a.initialize = func(context.Context, sdk.InitializeRequest) (sdk.InitializeResponse, error) {
		return sdk.InitializeResponse{ProtocolVersion: sdk.ProtocolVersionNumber, Meta: acpmeta.ForkMetadata(acpmeta.ForkTurn), AgentCapabilities: sdk.AgentCapabilities{SessionCapabilities: sdk.SessionCapabilities{Fork: acpmeta.ForkCapability(acpmeta.ForkTurn), AdditionalDirectories: &sdk.SessionAdditionalDirectoriesCapabilities{}}}}, nil
	}
	a.fork = func(_ context.Context, r sdk.UnstableForkSessionRequest) (sdk.UnstableForkSessionResponse, error) {
		got, err := acpmeta.ForkMCPServers(r.McpServers)
		if err != nil || !reflect.DeepEqual(got, servers) || r.Cwd != workspace.Root || !reflect.DeepEqual(r.AdditionalDirectories, workspace.Directories) {
			t.Errorf("lost fork parameters: %+v %v", r, err)
		}
		return sdk.UnstableForkSessionResponse{SessionId: "child"}, nil
	}
	_, err := openManagement(t, a).Fork(context.Background(), driver.ForkRequest{SourceSessionID: "source", Workspace: workspace, MCPServers: servers})
	if err != nil {
		t.Fatal(err)
	}
}

// TestManagementInitializeResponseReadsHandshakeWithoutSession 防止宿主读取能力时额外创建会话或触发 Prompt。
func TestManagementInitializeResponseReadsHandshakeWithoutSession(t *testing.T) {
	a := newFixtureAgent()
	var initializations, sessions, prompts atomic.Int32
	a.initialize = func(context.Context, sdk.InitializeRequest) (sdk.InitializeResponse, error) {
		initializations.Add(1)
		return sdk.InitializeResponse{ProtocolVersion: sdk.ProtocolVersionNumber, AgentInfo: &sdk.Implementation{Name: "management-fixture", Version: "1"}, AgentCapabilities: sdk.AgentCapabilities{McpCapabilities: sdk.McpCapabilities{Http: true, Sse: true}}}, nil
	}
	a.newSession = func(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
		sessions.Add(1)
		return sdk.NewSessionResponse{SessionId: "unexpected"}, nil
	}
	a.prompt = func(context.Context, sdk.PromptRequest) (sdk.PromptResponse, error) {
		prompts.Add(1)
		return sdk.PromptResponse{}, nil
	}
	m := openManagement(t, a)
	for range 2 {
		got := m.InitializeResponse()
		if got.ProtocolVersion != sdk.ProtocolVersionNumber || got.AgentInfo == nil || got.AgentInfo.Name != "management-fixture" || !got.AgentCapabilities.McpCapabilities.Http || !got.AgentCapabilities.McpCapabilities.Sse {
			t.Fatalf("handshake facts=%+v", got)
		}
	}
	if initializations.Load() != 1 || sessions.Load() != 0 || prompts.Load() != 0 {
		t.Fatalf("calls initialize=%d session=%d prompt=%d", initializations.Load(), sessions.Load(), prompts.Load())
	}
}

// TestManagementProbeSkipsOnlyRequestedModelInspection 验证宿主限定读取范围只禁止指定 setter，不丢弃原始目录或已知配置。
func TestManagementProbeSkipsOnlyRequestedModelInspection(t *testing.T) {
	a := &managementAgent{fixtureAgent: newFixtureAgent()}
	model := selectionOption(sdk.SessionConfigOptionCategoryModel, "model", "current", "current", "omitted", "known", "visible")
	reasoning := selectionOption(sdk.SessionConfigOptionCategoryThoughtLevel, "reasoning", "high", "high")
	(*model.Select.Options.Ungrouped)[2].Meta = acpmeta.WithModelConfigOptions(nil, []sdk.SessionConfigOption{reasoning})
	initial := []sdk.SessionConfigOption{model}
	var selected []string
	a.newSession = func(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
		return sdk.NewSessionResponse{SessionId: "probe", ConfigOptions: initial}, nil
	}
	a.configure = func(_ context.Context, r sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
		selected = append(selected, string(r.ValueId.Value))
		if r.ValueId.Value != "visible" {
			return sdk.SetSessionConfigOptionResponse{}, sdk.NewInvalidParams(nil)
		}
		return sdk.SetSessionConfigOptionResponse{ConfigOptions: []sdk.SessionConfigOption{selectionOption(sdk.SessionConfigOptionCategoryModel, "model", "visible", "visible"), reasoning}}, nil
	}
	got, err := openManagement(t, a).Probe(context.Background(), driver.ProbeRequest{Workspace: contract.WorkspaceScope{Root: t.TempDir()}, ModelConfigID: "model", InspectModelOptions: true, SkipModelOptions: []string{"current", "omitted", "known"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(selected, []string{"visible"}) {
		t.Fatalf("setter selection=%v", selected)
	}
	if len(got.Models) != 4 || got.Models[0].Model != "current" || got.Models[1].Model != "omitted" || got.Models[2].Model != "known" || got.Models[3].Model != "visible" {
		t.Fatalf("original catalog changed: %+v", got.Models)
	}
	if !managementOptionsEqual(got.Models[0].ConfigOptions, initial) || got.Models[1].ConfigOptions != nil || !managementOptionsEqual(got.Models[2].ConfigOptions, []sdk.SessionConfigOption{reasoning}) || len(got.Models[3].ConfigOptions) != 2 {
		t.Fatalf("configuration facts lost: %+v", got.Models)
	}
}
