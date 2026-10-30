package workspace

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
)

// PluginEnvironmentRequest 分开提供已准备的模型身份、原生环境事实及插件交付资源。
type PluginEnvironmentRequest struct {
	// Target 是账号与软件能力准备后的私有进程启动事实。
	Target runtimeconfig.RuntimeTarget
	// Source 是智能体环境保存的原生配置源，不受模型账号投射影响。
	Source runtimeconfig.RuntimeTarget
	// Detection 是已有探测确认的基础智能体和真实程序。
	Detection runtimeconfig.DetectionResult
	// Workspace 是聊天拥有的受管工作区。
	Workspace string
	// DataDirectory 是插件不可变内容和原生派生目录所属的数据根目录。
	DataDirectory string
	// Native 是已按插件租约冻结的原生包。
	Native []plugins.NativeTarget
	// Snapshots 是待补充原生交付身份的固定插件事实。
	Snapshots []plugins.Snapshot
	// Environment 是插件交付所需的私有环境变量。
	Environment map[string]string
	// AllowExternal 决定是否允许当前原生环境的外部插件。
	AllowExternal bool
}

// PluginEnvironmentResult 返回原生交付完成的启动事实和待接纳的作用域标记。
type PluginEnvironmentResult struct {
	// Target 是模型身份、原生插件和管理命令路径合并后的启动事实。
	Target runtimeconfig.RuntimeTarget
	// Snapshots 是含实际原生插件身份的交付事实。
	Snapshots []plugins.Snapshot
	// CommitScope 只在业务接纳后保存本次原生能力摘要。
	CommitScope func() error
	// ScopeChanged 表示原生配置相对上次接纳已经变化。
	ScopeChanged bool
}

// PreparePluginEnvironment 拥有厂商配置根、原生插件交付及管理命令环境的装配规则。
func PreparePluginEnvironment(input PluginEnvironmentRequest) (PluginEnvironmentResult, error) {
	result := PluginEnvironmentResult{Target: input.Target, Snapshots: slices.Clone(input.Snapshots)}
	result.Target.Environment = maps.Clone(input.Target.Environment)
	if result.Target.Environment == nil {
		result.Target.Environment = map[string]string{}
	}
	cliID := input.Detection.Runtime.BaseRuntimeID
	if cliID == "" {
		cliID = input.Detection.Runtime.ID
	}
	nativeCommands := ""
	if cliID == constants.RuntimeIDCodex || cliID == constants.RuntimeIDClaude {
		if cliID == constants.RuntimeIDCodex {
			if err := plugins.PrepareCodexWorkspace(input.Workspace, input.Native); err != nil {
				return result, err
			}
		}
		variable, defaultDirectory := "CODEX_HOME", ".codex"
		if cliID == constants.RuntimeIDClaude {
			variable, defaultDirectory = "CLAUDE_CONFIG_DIR", ".claude"
		}
		sharedHome := input.Source.ConfigHome
		if sharedHome == "" {
			sharedHome = os.Getenv(variable)
		}
		if sharedHome == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return result, status_error.PluginStorageError
			}
			sharedHome = filepath.Join(home, defaultDirectory)
		}
		sharedHome, err := filepath.Abs(sharedHome)
		if err != nil {
			return result, status_error.PluginStorageError
		}
		binary := input.Detection.ResolvedBinary
		if binary == "" {
			binary = input.Source.Binary
		}
		if binary == "" {
			binary = cliID
		}
		executable, err := exec.LookPath(binary)
		if err == nil {
			executable, err = filepath.Abs(executable)
		}
		if err != nil {
			return result, status_error.RuntimeUnavailable
		}
		accountHome := ""
		secretKeys := []string{}
		if result.Target.AccountID != "" {
			accountHome = result.Target.Environment[variable]
			for key := range result.Target.Environment {
				if key != "PATH" {
					secretKeys = append(secretKeys, key)
				}
			}
			secretKeys = append(secretKeys, "OPENAI_API_KEY", "CODEX_API_KEY", "CODEX_ACCESS_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL")
		}
		for key := range input.Environment {
			if key != "PATH" {
				secretKeys = append(secretKeys, key)
			}
		}
		native, err := plugins.PrepareNativeEnvironment(filepath.Join(input.DataDirectory, "plugins", "native-homes"), plugins.NativeEnvironmentSpec{
			RuntimeID: cliID, AccountHome: accountHome, SharedPluginHome: sharedHome, Executable: executable,
			PrefixArgs: input.Source.PrefixArgs, Targets: input.Native, AllowExternal: input.AllowExternal,
			ModelEnvironmentKeys: secretKeys, StateDirectory: filepath.Join(input.Workspace, constants.RuntimeStateDirectoryName, constants.PluginNativeScopeDirectory), Workspace: input.Workspace,
		})
		if err != nil {
			return result, err
		}
		result.Target.Environment[variable] = native.Home
		maps.Copy(result.Target.Environment, native.Environment)
		result.Target.ConfigHome = native.Home
		result.Target.CodexPluginConfiguration = native.CodexConfiguration
		result.Target.NativePluginScope = native.Fingerprint
		result.Target.ClaudeSettingsPath = native.ClaudeSettingsPath
		result.CommitScope, result.ScopeChanged = native.CommitScope, native.ScopeChanged
		if native.Marketplace != "" {
			if err := plugins.PrepareCodexWorkspace(input.Workspace, input.Native, native.Marketplace); err != nil {
				return result, err
			}
			for index := range result.Snapshots {
				if result.Snapshots[index].Delivery != constants.PluginDeliveryNative {
					continue
				}
				for _, target := range input.Native {
					if target.ID == result.Snapshots[index].ID {
						result.Snapshots[index].NativePluginID = target.Name + "@" + native.Marketplace
					}
				}
			}
		}
		nativeCommands = native.CommandDirectory
		if cliID == constants.RuntimeIDClaude {
			result.Target.NativePluginDirectories = slices.Clone(result.Target.NativePluginDirectories)
			for _, target := range input.Native {
				result.Target.NativePluginDirectories = append(result.Target.NativePluginDirectories, target.Root)
			}
		}
	}
	for key, value := range input.Environment {
		if key == "PATH" {
			prependPluginPath(result.Target.Environment, value)
			continue
		}
		result.Target.Environment[key] = value
	}
	if nativeCommands != "" {
		prependPluginPath(result.Target.Environment, nativeCommands)
	}
	return result, nil
}

// prependPluginPath 保留已有账号和软件能力入口，再按交付顺序添加插件命令。
func prependPluginPath(environment map[string]string, directory string) {
	base := environment["PATH"]
	if base == "" {
		base = os.Getenv("PATH")
	}
	environment["PATH"] = directory + string(os.PathListSeparator) + base
}
