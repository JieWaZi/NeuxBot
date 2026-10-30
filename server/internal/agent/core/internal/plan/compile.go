// Package plan 校验并冻结会话配置、本轮输入与有效权限。
package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Policies 是编译阶段的有效策略视图，公共合同分别属于会话与本轮输入。
type Policies struct {
	Tools        c.ToolPolicy         // Tools 是工具策略。
	NativeConfig c.NativeConfigPolicy // NativeConfig 是原生配置策略。
	Interaction  c.InteractionPolicy  // Interaction 是交互策略。
	Output       c.OutputPolicy       // Output 是结果证据策略。
}

// Spec 是仅供内核编译的冻结视图，不能作为公共执行入口。
type Spec struct {
	Profile       c.Profile          // Profile 是冻结技术选择。
	Input         c.Input            // Input 是本轮增量输入。
	Seed          []c.SeedMessage    // Seed 是仅供新建会话的有界历史播种。
	Session       c.SessionDirective // Session 是会话操作。
	Workspace     c.WorkspaceScope   // Workspace 是请求工作范围。
	Contributions c.Contributions    // Contributions 是静态能力贡献。
	Policies      Policies           // Policies 是独立执行策略。
	Limits        c.Limits           // Limits 是调用预算。
}

// Plan 隐藏所有可变切片，读取时复制。
type Plan struct {
	spec Spec // spec 是深冻结后的有效描述。
}

// Spec 返回不可影响计划的深复制描述。
func (p Plan) Spec() Spec { return clone(p.spec) }

// DefaultLimits 返回首批工程保护值，累计预算不等同于 ACP 排队预算。
func DefaultLimits() c.Limits {
	return c.Limits{MaxOutputBytes: 1 << 20, MaxEventBytes: 1 << 20, MaxEvents: 16384, MaxTotalEventBytes: 64 << 20, DeliveryTimeout: 5 * time.Second, InteractionTimeout: 5 * time.Minute, InactivityTimeout: 10 * time.Minute}
}

// NormalizeLimits 补全零值并拒绝负值。
func NormalizeLimits(v, defaults c.Limits) (c.Limits, error) {
	if v.MaxOutputBytes < 0 || v.MaxEventBytes < 0 || v.MaxEvents < 0 || v.MaxTotalEventBytes < 0 || v.DeliveryTimeout < 0 || v.InteractionTimeout < 0 || v.InactivityTimeout < 0 {
		return v, bad("negative execution budget")
	}
	if v.MaxOutputBytes == 0 {
		v.MaxOutputBytes = defaults.MaxOutputBytes
	}
	if v.MaxEventBytes == 0 {
		v.MaxEventBytes = defaults.MaxEventBytes
	}
	if v.MaxEvents == 0 {
		v.MaxEvents = defaults.MaxEvents
	}
	if v.MaxTotalEventBytes == 0 {
		v.MaxTotalEventBytes = defaults.MaxTotalEventBytes
	}
	if v.DeliveryTimeout == 0 {
		v.DeliveryTimeout = defaults.DeliveryTimeout
	}
	if v.InteractionTimeout == 0 {
		v.InteractionTimeout = defaults.InteractionTimeout
	}
	if v.InactivityTimeout == 0 {
		v.InactivityTimeout = defaults.InactivityTimeout
	}
	if v.MaxOutputBytes > v.MaxTotalEventBytes || v.MaxEventBytes > v.MaxTotalEventBytes {
		return v, bad("event and output budgets exceed total event budget")
	}
	return v, nil
}

// Compile 校验能力、收敛权限并产生不含秘密的稳定计划。
func Compile(input Spec, caps c.DriverCapabilities, grants c.Grants, defaults c.Limits) (Plan, error) {
	// 在深复制前核算历史预算，避免超限输入先造成无界复制。
	if len(input.Seed) > 40 {
		return Plan{}, bad("seed message count exceeds twenty complete turns")
	}
	seedBytes := 0
	for _, seed := range input.Seed {
		if len(seed.Text) > (64<<10)-seedBytes {
			return Plan{}, bad("seed text budget exceeded")
		}
		seedBytes += len(seed.Text)
		for _, reference := range seed.CodeReferences {
			for _, text := range []string{reference.Path, reference.Content} {
				if len(text) > (64<<10)-seedBytes {
					return Plan{}, bad("seed text budget exceeded")
				}
				seedBytes += len(text)
			}
		}
	}
	s := clone(input)
	var err error
	if s.Profile.DriverID == "" {
		return Plan{}, bad("driver identity missing")
	}
	if s.Session.Mode == "" {
		s.Session.Mode = c.SessionNew
	}
	switch s.Session.Mode {
	case c.SessionNew:
		if s.Session.NativeHandle != "" {
			return Plan{}, bad("new session cannot include a native identity")
		}
	case c.SessionResume:
		if len(s.Seed) > 0 {
			return Plan{}, bad("resume cannot replay seed history")
		}
		// 这里不拥有连接状态；冷恢复的能力由 Runtime 在 Bind 时检查。
		if !caps.HotResume && !caps.ColdResume {
			return Plan{}, unsupported("driver session resume is not verified")
		}
	default:
		return Plan{}, bad("unknown session mode")
	}
	for _, seed := range s.Seed {
		if seed.Role != "user" && seed.Role != "assistant" {
			return Plan{}, bad("unsupported seed role")
		}
	}
	if s.Policies.Output == "" {
		s.Policies.Output = c.OutputStrict
	}
	if s.Policies.Output != c.OutputStrict && s.Policies.Output != c.OutputBestEffort {
		return Plan{}, bad("unknown output policy")
	}
	if s.Policies.Interaction == "" {
		s.Policies.Interaction = c.InteractionUnattended
	}
	if s.Policies.Interaction != c.InteractionUnattended && s.Policies.Interaction != c.InteractionInteractive {
		return Plan{}, bad("unknown interaction policy")
	}
	if s.Policies.NativeConfig == "" {
		s.Policies.NativeConfig = c.NativeConfigExplicitOnly
	}
	switch s.Policies.NativeConfig {
	case c.NativeConfigExplicitOnly:
		if !caps.ExplicitConfig {
			return Plan{}, unsupported("explicit native configuration is not verified")
		}
	case c.NativeConfigInherit:
	default:
		return Plan{}, bad("unknown native configuration policy")
	}
	if s.Policies.Tools.Mode == "" {
		s.Policies.Tools.Mode = c.ToolsDenyAll
	}
	switch s.Policies.Tools.Mode {
	case c.ToolsDenyAll:
		s.Policies.Tools.Allow = nil
	case c.ToolsAllowAll:
		if !grants.AllowAllTools {
			s.Policies.Tools.Mode = c.ToolsAllowList
			s.Policies.Tools.Allow = slices.Clone(grants.Tools)
		} else {
			s.Policies.Tools.Allow = nil
		}
	case c.ToolsAllowList:
		allow := []string{}
		for _, name := range s.Policies.Tools.Allow {
			if name != "" && (grants.AllowAllTools || slices.Contains(grants.Tools, name)) {
				allow = append(allow, name)
			}
		}
		s.Policies.Tools.Allow = allow
	default:
		return Plan{}, bad("unknown tool policy")
	}
	if s.Policies.Tools.Mode == c.ToolsAllowList {
		slices.Sort(s.Policies.Tools.Allow)
		s.Policies.Tools.Allow = slices.Compact(s.Policies.Tools.Allow)
		if len(s.Policies.Tools.Allow) == 0 {
			s.Policies.Tools.Mode = c.ToolsDenyAll
		}
	}
	if s.Policies.Tools.Mode == c.ToolsDenyAll && !caps.DenyAllTools {
		return Plan{}, unsupported("deny all tools is not verified")
	}
	if s.Policies.Tools.Mode == c.ToolsAllowList && !caps.ToolAllowList {
		return Plan{}, unsupported("tool allow list is not verified")
	}
	if s.Workspace.Root != "" {
		s.Workspace.Root, err = directory(s.Workspace.Root, grants.Directories)
		if err != nil {
			return Plan{}, err
		}
	}
	for i, path := range s.Workspace.Directories {
		s.Workspace.Directories[i], err = directory(path, grants.Directories)
		if err != nil {
			return Plan{}, err
		}
	}
	slices.Sort(s.Workspace.Directories)
	s.Workspace.Directories = slices.Compact(s.Workspace.Directories)
	effective := slices.Clone(s.Workspace.Directories)
	if s.Workspace.Root != "" {
		effective = append(effective, s.Workspace.Root)
	}
	for _, a := range s.Input.Attachments {
		if _, err = directory(a.Path, effective); err != nil {
			return Plan{}, bad("attachment is outside effective workspace")
		}
	}
	if err = contributions(&s.Contributions); err != nil {
		return Plan{}, err
	}
	defaults, err = NormalizeLimits(defaults, DefaultLimits())
	if err != nil {
		return Plan{}, err
	}
	s.Limits, err = NormalizeLimits(s.Limits, defaults)
	if err != nil {
		return Plan{}, err
	}
	return Plan{spec: s}, nil
}

func contributions(v *c.Contributions) error {
	instructions := make(map[string]c.Instruction)
	out := []c.Instruction{}
	for _, x := range v.Instructions {
		if x.Name == "" {
			return bad("instruction identity missing")
		}
		if old, ok := instructions[x.Name]; ok {
			if old != x {
				return bad("conflicting instruction contribution")
			}
			continue
		}
		instructions[x.Name] = x
		out = append(out, x)
	}
	v.Instructions = out
	names := make(map[string]string)
	skills := []c.Skill{}
	for _, x := range v.Skills {
		if x.Name == "" || x.ResourceID == "" {
			return bad("skill identity missing")
		}
		key := "skill:" + x.Name
		value := digest(x)
		if old, ok := names[key]; ok {
			if old != value {
				return bad("conflicting skill contribution")
			}
			continue
		}
		names[key] = value
		skills = append(skills, x)
	}
	v.Skills = skills
	mcp := []c.MCP{}
	for _, x := range v.MCP {
		if x.Name == "" || x.ResourceID == "" {
			return bad("mcp identity missing")
		}
		key := "mcp:" + x.Name
		value := digest(x)
		if old, ok := names[key]; ok {
			if old != value {
				return bad("conflicting mcp contribution")
			}
			continue
		}
		names[key] = value
		mcp = append(mcp, x)
	}
	v.MCP = mcp
	return nil
}
func directory(path string, grants []string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", bad("workspace path must be absolute")
	}
	resolved, err := canonicalPath(path)
	if err != nil {
		return "", bad("workspace path cannot be resolved")
	}
	for _, root := range grants {
		if !filepath.IsAbs(root) {
			continue
		}
		resolvedRoot, err := canonicalPath(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(resolvedRoot, resolved)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return resolved, nil
		}
	}
	return "", bad("workspace path is not granted")
}

// canonicalPath 对尚不存在的叶节点解析最近已存在祖先，拒绝当前符号链接逃逸。
// 文件系统后续变化仍由宿主快照租约和 Driver 原生隔离边界控制。
func canonicalPath(path string) (string, error) {
	current := filepath.Clean(path)
	var suffix []string
	for {
		if _, err := os.Lstat(current); err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
		parent := filepath.Dir(current)
		if parent == current {
			return "", bad("path has no resolvable ancestor")
		}
		current = parent
	}
}

func clone(v Spec) Spec {
	if v.Profile.Thinking != nil {
		thinking := *v.Profile.Thinking
		v.Profile.Thinking = &thinking
	}
	v.Input.Attachments = slices.Clone(v.Input.Attachments)
	v.Input.CodeReferences = slices.Clone(v.Input.CodeReferences)
	v.Input.SelectedSkills = slices.Clone(v.Input.SelectedSkills)
	v.Input.SelectedMCP = slices.Clone(v.Input.SelectedMCP)
	v.Seed = slices.Clone(v.Seed)
	for index := range v.Seed {
		v.Seed[index].CodeReferences = slices.Clone(v.Seed[index].CodeReferences)
	}
	v.Workspace.Directories = slices.Clone(v.Workspace.Directories)
	v.Contributions.Instructions = slices.Clone(v.Contributions.Instructions)
	v.Contributions.Skills = slices.Clone(v.Contributions.Skills)
	v.Contributions.MCP = slices.Clone(v.Contributions.MCP)
	v.Policies.Tools.Allow = slices.Clone(v.Policies.Tools.Allow)
	return v
}

func digest(v any) string {
	data, _ := json.Marshal(v)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func bad(message string) *c.Fault {
	return &c.Fault{Code: c.FaultInvalidSpec, Stage: "plan", Message: message}
}
func unsupported(message string) *c.Fault {
	return &c.Fault{Code: c.FaultUnsupported, Stage: "plan", Message: message}
}

// CloneSession 深复制稳定配置，防止调用方在接纳后改写兼容事实。
func CloneSession(value c.SessionPlan) c.SessionPlan {
	bridge := clone(Spec{Profile: value.Profile, Workspace: value.Workspace, Contributions: value.Contributions, Policies: Policies{Tools: value.Policies.Tools}})
	value.Profile, value.Workspace, value.Contributions = bridge.Profile, bridge.Workspace, bridge.Contributions
	value.Policies.Tools = bridge.Policies.Tools
	value.Configuration.Arguments = slices.Clone(value.Configuration.Arguments)
	value.Configuration.PluginDirectories = slices.Clone(value.Configuration.PluginDirectories)
	if value.Configuration.Options != nil {
		options := make(map[string]string, len(value.Configuration.Options))
		for key, item := range value.Configuration.Options {
			options[key] = item
		}
		value.Configuration.Options = options
	}
	value.Grants.Directories = slices.Clone(value.Grants.Directories)
	value.Grants.Tools = slices.Clone(value.Grants.Tools)
	value.Tags = slices.Clone(value.Tags)
	return value
}
