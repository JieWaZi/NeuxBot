package catalog

import (
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"

	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	acp "github.com/JieWaZi/acp-go/sdk"
)

// TestQoderCNRegistryAndIsolation 验证中国版与国际版分别保存命令和用户配置根，共用项目技能目录，共用同一权限契约。
func TestQoderCNRegistryAndIsolation(t *testing.T) {
	definitions, err := newRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	definition := definitions[runtimecontract.RuntimeIDQoderCN]
	if definition.name != "Qoder CN" || definition.instructionFileName != "AGENTS.md" || definition.skillDirectoryName != ".qoder" || definition.configHomeEnvironmentVariable != "QODERCN_CONFIG_DIR" || !reflect.DeepEqual(definition.binaryCandidates, []string{"qodercn", "qoder-cn", "qoderclicn"}) || len(definition.models) != 0 {
		t.Fatalf("incorrect CN definition: %+v", definition)
	}
	if !slices.Contains(BaseRuntimeIDs(), runtimecontract.RuntimeIDQoderCN) {
		t.Fatal("connector matrix omitted Qoder CN")
	}
	binding := defaultACPRuntimeBindings()[runtimecontract.RuntimeIDQoderCN]
	if binding.baseRuntimeID != runtimecontract.RuntimeIDQoderCN || binding.displayName != "Qoder CN" || !binding.nativeProcess {
		t.Fatal("CN identity or process isolation lost")
	}
	for _, transport := range []runtimecontract.ConnectorTransport{runtimecontract.ConnectorTransportStdio, runtimecontract.ConnectorTransportStreamableHTTP, runtimecontract.ConnectorTransportSSE} {
		if supported, _ := ConnectorCompatibility(runtimecontract.RuntimeIDQoderCN, transport); !supported {
			t.Fatalf("missing transport %s", transport)
		}
	}
	for flag := range protectedPrefixArguments[runtimecontract.RuntimeIDQoder] {
		if LaunchOverrideArgumentsAllowed(runtimecontract.RuntimeIDQoderCN, []string{flag + "=override"}) {
			t.Fatalf("unsafe CN override accepted: %s", flag)
		}
	}
}

// TestQoderNativeInstallDiscovery 验证 PATH 缺省时也能解析官方原生路径，两个地区与显式路径覆盖互不混用。
func TestQoderNativeInstallDiscovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX executable scripts")
	}
	for _, id := range []string{runtimecontract.RuntimeIDQoder, runtimecontract.RuntimeIDQoderCN} {
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
			request := runtimecontract.DetectRequest{RuntimeID: id, HomeDirectory: home, BaseEnvironment: []string{"PATH="}}
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
			other := runtimecontract.RuntimeIDQoder
			if id == other {
				other = runtimecontract.RuntimeIDQoderCN
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
	for _, id := range []string{runtimecontract.RuntimeIDQoder, runtimecontract.RuntimeIDQoderCN} {
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
		request := runtimecontract.DetectRequest{RuntimeID: id, HomeDirectory: home, BaseEnvironment: []string{"PATH="}}
		if _, err := sdk.ResolveExecutable(request); err == nil {
			t.Fatal("desktop dispatcher treated as installed CLI")
		}
	}
}

// TestQoderRegistryAndPermissionCatalog 验证原生目录、传输和实际模式目录。
func TestQoderRegistryAndPermissionCatalog(t *testing.T) {
	definitions, err := newRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	definition := definitions[runtimecontract.RuntimeIDQoder]
	if definition.name != "Qoder" || definition.instructionFileName != "AGENTS.md" || definition.skillDirectoryName != ".qoder" || definition.configHomeEnvironmentVariable != "QODER_CONFIG_DIR" || !reflect.DeepEqual(definition.binaryCandidates, []string{"qoder", "qodercli"}) || len(definition.models) != 0 {
		t.Fatalf("native definition: %+v", definition)
	}
	binding := defaultACPRuntimeBindings()[runtimecontract.RuntimeIDQoder]
	if binding.newAgent == nil || !binding.nativeProcess {
		t.Fatal("missing native factory")
	}
	for _, bypass := range []string{constants.RuntimeQoderBypassModeID, constants.RuntimeQoderYoloModeID} {
		modes := []acp.SessionMode{{Id: "default"}, {Id: acp.SessionModeId(bypass)}}
		if got := binding.permissionLevels(nil, modes); !reflect.DeepEqual(got, []runtimecontract.PermissionLevel{runtimecontract.PermissionLevelDefault, runtimecontract.PermissionLevelFullAccess}) {
			t.Fatalf("invented permissions: %v", got)
		}
	}
	modes := []acp.SessionMode{{Id: "default"}, {Id: "auto"}, {Id: "yolo"}}
	if got := binding.permissionLevels(nil, modes); !reflect.DeepEqual(got, []runtimecontract.PermissionLevel{runtimecontract.PermissionLevelDefault, runtimecontract.PermissionLevelAuto, runtimecontract.PermissionLevelFullAccess}) {
		t.Fatalf("auto mode lost: %v", got)
	}
	for _, transport := range []runtimecontract.ConnectorTransport{runtimecontract.ConnectorTransportStdio, runtimecontract.ConnectorTransportStreamableHTTP, runtimecontract.ConnectorTransportSSE} {
		if supported, _ := ConnectorCompatibility(runtimecontract.RuntimeIDQoder, transport); !supported {
			t.Fatalf("missing transport: %s", transport)
		}
	}
	for flag := range protectedPrefixArguments[runtimecontract.RuntimeIDQoder] {
		if LaunchOverrideArgumentsAllowed(runtimecontract.RuntimeIDQoder, []string{flag}) || LaunchOverrideArgumentsAllowed(runtimecontract.RuntimeIDQoder, []string{flag + "=override"}) {
			t.Fatalf("unsafe override: %s", flag)
		}
	}
}
