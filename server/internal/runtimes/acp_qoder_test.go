package runtimes

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	acp "github.com/coder/acp-go-sdk"
)

// TestQoderRegistryAndPermissions 覆盖原生目录、传输、严格权限菜单与前置参数保护。
func TestQoderRegistryAndPermissions(t *testing.T) {
	definitions, err := newRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	definition := definitions[RuntimeIDQoder]
	if definition.name != "Qoder" || definition.instructionFileName != "AGENTS.md" || definition.skillDirectoryName != ".qoder" || definition.configHomeEnvironmentVariable != "QODER_CONFIG_DIR" || !reflect.DeepEqual(definition.binaryCandidates, []string{"qoder", "qodercli"}) || len(definition.models) != 0 {
		t.Fatalf("incorrect native definition: %+v", definition)
	}
	if !slices.Contains(BaseRuntimeIDs(), RuntimeIDQoder) {
		t.Fatal("connector matrix omitted Qoder")
	}
	binding := defaultACPRuntimeBindings()[RuntimeIDQoder]
	if !binding.nativeProcess || binding.retainedProcess {
		t.Fatal("Qoder process must be isolated per invocation")
	}
	for _, transport := range []ConnectorTransport{ConnectorTransportStdio, ConnectorTransportStreamableHTTP, ConnectorTransportSSE} {
		if supported, _ := ConnectorCompatibility(RuntimeIDQoder, transport); !supported {
			t.Fatalf("missing transport %s", transport)
		}
	}
	for _, bypass := range []string{constants.RuntimeQoderBypassModeID, constants.RuntimeQoderYoloModeID} {
		modes := &acp.SessionModeState{AvailableModes: []acp.SessionMode{{Id: "default"}, {Id: acp.SessionModeId(bypass)}}}
		if got := binding.permissionLevels(nil, modes.AvailableModes); !reflect.DeepEqual(got, []PermissionLevel{PermissionLevelDefault, PermissionLevelFullAccess}) {
			t.Fatalf("invented permissions: %v", got)
		}
		for _, level := range []PermissionLevel{PermissionLevelDefault, PermissionLevelFullAccess} {
			want := acp.SessionModeId("default")
			if level == PermissionLevelFullAccess {
				want = acp.SessionModeId(bypass)
			}
			if got, err := binding.modeID(ExecutionPolicy{PermissionLevel: level}, modes); err != nil || got != want {
				t.Fatalf("mode = %s, %v", got, err)
			}
		}
		for _, policy := range []ExecutionPolicy{{PermissionLevel: PermissionLevelAuto}, {PermissionLevel: PermissionLevelFullAccess, WorkspaceReadOnly: true}} {
			if _, err := binding.modeID(policy, modes); err == nil {
				t.Fatalf("unsupported policy accepted: %+v", policy)
			}
		}
	}
	autoModes := &acp.SessionModeState{AvailableModes: []acp.SessionMode{{Id: "default"}, {Id: "auto"}, {Id: "yolo"}}}
	if got := binding.permissionLevels(nil, autoModes.AvailableModes); !reflect.DeepEqual(got, []PermissionLevel{PermissionLevelDefault, PermissionLevelAuto, PermissionLevelFullAccess}) {
		t.Fatalf("advertised auto mode lost: %v", got)
	}
	if got, err := binding.modeID(ExecutionPolicy{PermissionLevel: PermissionLevelAuto}, autoModes); err != nil || got != "auto" {
		t.Fatalf("advertised auto mode = %s, %v", got, err)
	}
	if _, err := binding.modeID(ExecutionPolicy{PermissionLevel: PermissionLevelFullAccess}, &acp.SessionModeState{AvailableModes: []acp.SessionMode{{Id: "default"}}}); err == nil {
		t.Fatal("missing bypass mode accepted")
	}
	if _, err := binding.modeID(ExecutionPolicy{}, nil); err == nil {
		t.Fatal("missing mode catalog accepted")
	}
	for flag := range protectedPrefixArguments[RuntimeIDQoder] {
		if LaunchOverrideArgumentsAllowed(RuntimeIDQoder, []string{flag}) || LaunchOverrideArgumentsAllowed(RuntimeIDQoder, []string{flag + "=override"}) {
			t.Fatalf("unsafe override accepted: %s", flag)
		}
	}
	if !LaunchOverrideArgumentsAllowed(RuntimeIDQoder, []string{"--debug"}) {
		t.Fatal("diagnostic flag rejected")
	}
}

// TestQoderReappliesPermissionOnResume 确保续聊重建进程后回落默认权限，不继承历史完全访问。
func TestQoderReappliesPermissionOnResume(t *testing.T) {
	backend := newACPBackend()
	defer backend.Close(context.Background())
	agents := []*fakeCodexACPAgent{}
	replaceACPAgentFactory(backend, RuntimeIDQoder, func(_ context.Context, _ acpAdapterConfig) (acp.Agent, error) {
		agent := &fakeCodexACPAgent{sessionID: "qoder", selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId), simplePrompt: true}
		agents = append(agents, agent)
		return &qoderFixtureAgent{fakeCodexACPAgent: agent}, nil
	})
	request := matrixACPRunRequest(t, RuntimeIDQoder, SessionDirective{Mode: SessionModeNew})
	for _, level := range []PermissionLevel{PermissionLevelFullAccess, PermissionLevelDefault} {
		request.Request.Policy.Execution.PermissionLevel = level
		result := backend.Run(context.Background(), request)
		if result.Failure != nil {
			t.Fatalf("%s: %+v", level, result)
		}
		request.Request.Session = SessionDirective{Mode: SessionModeResume, ID: result.Session.OutputID}
	}
	if len(agents) != 2 || agents[0].selectedMode != "bypassPermissions" || agents[1].selectedMode != "default" {
		t.Fatal("resumed permission did not follow current policy")
	}
}

// qoderFixtureAgent 提供 Qoder 官方文档声明的两种 ACP 模式。
type qoderFixtureAgent struct {
	// fakeCodexACPAgent 复用标准 ACP 请求记录和回执。
	*fakeCodexACPAgent
}

// NewSession 在标准会话回执中补充 Qoder 权限目录。
func (agent *qoderFixtureAgent) NewSession(ctx context.Context, request acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	response, err := agent.fakeCodexACPAgent.NewSession(ctx, request)
	response.Modes = &acp.SessionModeState{AvailableModes: []acp.SessionMode{{Id: "default", Name: "Default"}, {Id: "bypassPermissions", Name: "Bypass"}}}
	return response, err
}

// ResumeSession 使恢复同样显式携带当前权限目录。
func (agent *qoderFixtureAgent) ResumeSession(ctx context.Context, request acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	response, err := agent.fakeCodexACPAgent.ResumeSession(ctx, request)
	response.Modes = &acp.SessionModeState{AvailableModes: []acp.SessionMode{{Id: "default", Name: "Default"}, {Id: "bypassPermissions", Name: "Bypass"}}}
	return response, err
}

// TestQoderCNRegistryAndIsolation 验证中国版与国际版分别保存命令和用户配置根，共用项目技能目录，共用同一权限契约。
func TestQoderCNRegistryAndIsolation(t *testing.T) {
	definitions, err := newRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	definition := definitions[RuntimeIDQoderCN]
	if definition.name != "Qoder CN" || definition.instructionFileName != "AGENTS.md" || definition.skillDirectoryName != ".qoder" || definition.configHomeEnvironmentVariable != "QODERCN_CONFIG_DIR" || !reflect.DeepEqual(definition.binaryCandidates, []string{"qodercn", "qoder-cn", "qoderclicn"}) || len(definition.models) != 0 {
		t.Fatalf("incorrect CN definition: %+v", definition)
	}
	if !slices.Contains(BaseRuntimeIDs(), RuntimeIDQoderCN) {
		t.Fatal("connector matrix omitted Qoder CN")
	}
	binding := defaultACPRuntimeBindings()[RuntimeIDQoderCN]
	if binding.baseRuntimeID != RuntimeIDQoderCN || binding.displayName != "Qoder CN" || !binding.nativeProcess || binding.retainedProcess {
		t.Fatal("CN identity or process isolation lost")
	}
	for _, transport := range []ConnectorTransport{ConnectorTransportStdio, ConnectorTransportStreamableHTTP, ConnectorTransportSSE} {
		if supported, _ := ConnectorCompatibility(RuntimeIDQoderCN, transport); !supported {
			t.Fatalf("missing transport %s", transport)
		}
	}
	for flag := range protectedPrefixArguments[RuntimeIDQoder] {
		if LaunchOverrideArgumentsAllowed(RuntimeIDQoderCN, []string{flag + "=override"}) {
			t.Fatalf("unsafe CN override accepted: %s", flag)
		}
	}
}

// TestQoderNativeInstallDiscovery 验证 PATH 缺省时也能解析官方原生路径，两个地区与显式路径覆盖互不混用。
func TestQoderNativeInstallDiscovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX executable scripts")
	}
	for _, id := range []string{RuntimeIDQoder, RuntimeIDQoderCN} {
		t.Run(id, func(t *testing.T) {
			home := t.TempDir()
			sdk, err := New(Config{})
			if err != nil {
				t.Fatal(err)
			}
			defer sdk.Close(context.Background())
			definition := sdk.definitions[id]
			binary := filepath.Join(home, definition.userBinaryCandidates[0])
			if err := os.MkdirAll(filepath.Dir(binary), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '1.2.3\\n'\n"), 0700); err != nil {
				t.Fatal(err)
			}
			request := DetectRequest{RuntimeID: id, HomeDirectory: home, BaseEnvironment: []string{"PATH="}}
			resolved, err := sdk.ResolveExecutable(request)
			if err != nil || resolved != binary {
				t.Fatalf("native executable = %s, %v", resolved, err)
			}
			installations := sdk.DiscoverInstallations(context.Background(), request, nil)
			found := false
			for _, installation := range installations {
				if installation.Path == binary && installation.Available && installation.Version == "1.2.3" {
					found = true
				}
			}
			if !found {
				t.Fatalf("native installation omitted: %+v", installations)
			}
			// 国际版与中国版之间不做透明登录或可执行文件替换。
			other := RuntimeIDQoder
			if id == other {
				other = RuntimeIDQoderCN
			}
			request.RuntimeID = other
			if _, err := sdk.ResolveExecutable(request); err == nil {
				t.Fatal("cross-region executable fallback")
			}
			request.RuntimeID = id
			request.LaunchOverride.Binary = filepath.Join(home, "missing-explicit-binary")
			if _, err := sdk.ResolveExecutable(request); err == nil {
				t.Fatal("explicit binary silently fell back to native installation")
			}
		})
	}
}

// TestQoderDesktopDispatcherIsNotNativeInstall 防止仅有桌面版 entry 分发脚本时误报安装了原生 CLI。
func TestQoderDesktopDispatcherIsNotNativeInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX executable scripts")
	}
	for _, id := range []string{RuntimeIDQoder, RuntimeIDQoderCN} {
		home := t.TempDir()
		sdk, err := New(Config{})
		if err != nil {
			t.Fatal(err)
		}
		defer sdk.Close(context.Background())
		definition := sdk.definitions[id]
		entry := filepath.Join(home, "."+id, "entry", definition.binaryCandidates[0])
		if err := os.MkdirAll(filepath.Dir(entry), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(entry, []byte("#!/bin/sh\nexit 127\n"), 0700); err != nil {
			t.Fatal(err)
		}
		request := DetectRequest{RuntimeID: id, HomeDirectory: home, BaseEnvironment: []string{"PATH="}}
		if _, err := sdk.ResolveExecutable(request); err == nil {
			t.Fatal("desktop dispatcher treated as installed CLI")
		}
	}
}
