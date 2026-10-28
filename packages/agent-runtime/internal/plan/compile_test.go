package plan

import (
	c "github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func spec() c.ExecutionSpec {
	return c.ExecutionSpec{Profile: c.Profile{DriverID: "fake", Subject: "a"}, Input: c.Input{Text: "first"}, Workspace: c.WorkspaceScope{Root: "/allowed/project"}, Policies: c.Policies{Tools: c.ToolPolicy{Mode: c.ToolsAllowList, Allow: []string{"read", "write"}}, NativeConfig: c.NativeConfigExplicitOnly, Output: c.OutputStrict}, Contributions: c.Contributions{Instructions: []c.Instruction{{Name: "profile", Text: "static"}}}}
}
func TestFreezeDigestsAndPermissionIntersection(t *testing.T) {
	in := spec()
	caps := c.DriverCapabilities{ExplicitConfig: true, ToolAllowList: true, HotResume: true}
	grants := c.Grants{Directories: []string{"/allowed"}, Tools: []string{"read"}}
	a, err := Compile(in, caps, grants, c.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	in.Input.Text = "second"
	in.Contributions.Instructions[0].Text = "changed"
	in.Policies.Tools.Allow[0] = "delete"
	f := a.Spec()
	if f.Input.Text != "first" || f.Contributions.Instructions[0].Text != "static" || len(f.Policies.Tools.Allow) != 1 || f.Policies.Tools.Allow[0] != "read" {
		t.Fatalf("not frozen: %#v", f)
	}
	f.Contributions.Instructions[0].Text = "mutated"
	if a.Spec().Contributions.Instructions[0].Text != "static" {
		t.Fatal("getter exposed storage")
	}
	second := spec()
	second.Input.Text = "second"
	second.InvocationID = "other"
	second.Session = c.SessionDirective{Mode: c.SessionResume, Ref: "s"}
	bb, err := Compile(second, caps, grants, c.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if a.ContextDigest() != bb.ContextDigest() || a.ExecutionDigest() == bb.ExecutionDigest() {
		t.Fatal("unstable static digest")
	}
	second.Profile.Subject = "b"
	cc, err := Compile(second, caps, grants, c.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if cc.SecurityScopeKey() == a.SecurityScopeKey() {
		t.Fatal("account isolation lost")
	}
}
func TestRejectResumeSeedAndUnknownEnforcement(t *testing.T) {
	in := spec()
	in.Session = c.SessionDirective{Mode: c.SessionResume, Ref: "s", Seed: []c.SeedMessage{{Role: "user", Text: "old"}}}
	if _, err := Compile(in, c.DriverCapabilities{ExplicitConfig: true, ToolAllowList: true}, c.Grants{Directories: []string{"/allowed"}}, c.Limits{}); err == nil {
		t.Fatal("resume seed accepted")
	}
	in = spec()
	if _, err := Compile(in, c.DriverCapabilities{}, c.Grants{}, c.Limits{}); err == nil {
		t.Fatal("unknown isolation accepted")
	}
	in = spec()
	in.Workspace.Root = "/outside"
	if _, err := Compile(in, c.DriverCapabilities{ExplicitConfig: true, ToolAllowList: true}, c.Grants{Directories: []string{"/allowed"}}, c.Limits{}); err == nil {
		t.Fatal("workspace expanded")
	}
}

// TestColdCheckpointRejectsChangedScopeBeforeNativeOpen 防止冷恢复绕过原生身份与静态配置的兼容检查。
func TestColdCheckpointRejectsChangedScopeBeforeNativeOpen(t *testing.T) {
	in := spec()
	capabilities := c.DriverCapabilities{Version: "v1", ExplicitConfig: true, ToolAllowList: true, ColdResume: true}
	grants := c.Grants{Directories: []string{"/allowed"}, Tools: []string{"read"}}
	initial, err := Compile(in, capabilities, grants, c.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := &c.SessionCheckpoint{Version: 1, SessionID: "session", DriverID: "fake", DriverVersion: "v1", NativeHandle: "native", ContextDigest: initial.ContextDigest(), SecurityScopeKey: initial.SecurityScopeKey()}
	in.Session = c.SessionDirective{Mode: c.SessionResume, Checkpoint: checkpoint}
	if _, err := Compile(in, capabilities, grants, c.Limits{}); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*c.ExecutionSpec){
		func(in *c.ExecutionSpec) { in.Session.Ref = "hot" },
		func(in *c.ExecutionSpec) { in.Session.Seed = []c.SeedMessage{{Role: "user", Text: "duplicate"}} },
		func(in *c.ExecutionSpec) { in.Profile.Model = "another" },
		func(in *c.ExecutionSpec) { in.Profile.Subject = "another" },
		func(in *c.ExecutionSpec) { in.Workspace.ReadOnly = true },
	} {
		changed := clone(in)
		change(&changed)
		if _, err := Compile(changed, capabilities, grants, c.Limits{}); err == nil {
			t.Fatal("changed checkpoint scope accepted")
		}
	}
}

func TestRejectSymlinkEscapeAndImpossibleBudgets(t *testing.T) {
	root := t.TempDir()
	allowed := filepath.Join(root, "allowed")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(allowed, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(allowed, "escape")); err != nil {
		t.Fatal(err)
	}
	in := spec()
	in.Workspace.Root = filepath.Join(allowed, "escape")
	grants := c.Grants{Directories: []string{allowed}, Tools: []string{"read"}}
	caps := c.DriverCapabilities{ExplicitConfig: true, ToolAllowList: true, DenyAllTools: true}
	if _, err := Compile(in, caps, grants, c.Limits{}); err == nil {
		t.Fatal("symlink expanded workspace outside authorization")
	}
	for _, limits := range []c.Limits{{MaxOutputBytes: 20, MaxTotalEventBytes: 10, MaxEventBytes: 10}, {MaxOutputBytes: 10, MaxEventBytes: 20, MaxTotalEventBytes: 10}} {
		in = spec()
		in.Limits = limits
		if _, err := Compile(in, caps, c.Grants{Directories: []string{"/allowed"}, Tools: []string{"read"}}, c.Limits{}); err == nil {
			t.Fatal("impossible event/output budget accepted")
		}
	}
}
func TestFreezePreservesExactTextBytes(t *testing.T) {
	in := spec()
	in.Input.Text = "byte\xff"
	p, err := Compile(in, c.DriverCapabilities{ExplicitConfig: true, ToolAllowList: true}, c.Grants{Directories: []string{"/allowed"}, Tools: []string{"read"}}, c.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Spec().Input.Text != "byte\xff" {
		t.Fatal("freezing transformed input bytes")
	}
}

func TestSeedBudgetAcceptsBoundaryAndCountsUTF8Bytes(t *testing.T) {
	in := spec()
	in.Session.Seed = make([]c.SeedMessage, 40)
	for i := range in.Session.Seed {
		in.Session.Seed[i] = c.SeedMessage{Role: "user"}
	}
	in.Session.Seed[0].Text = strings.Repeat("x", 64<<10)
	caps := c.DriverCapabilities{ExplicitConfig: true, ToolAllowList: true}
	grants := c.Grants{Directories: []string{"/allowed"}, Tools: []string{"read"}}
	if _, err := Compile(in, caps, grants, c.Limits{}); err != nil {
		t.Fatal(err)
	}
	in.Session.Seed[1].Text = "中"
	if _, err := Compile(in, caps, grants, c.Limits{}); err == nil {
		t.Fatal("UTF-8 bytes expanded beyond seed limit")
	}
}

// TestThinkingAndNativeModeFreezeInStaticContext 防止可变偏好指针改写已接纳计划或原生权限变化错误复用上下文。
func TestThinkingAndNativeModeFreezeInStaticContext(t *testing.T) {
	off := false
	input := spec()
	input.Profile.Thinking = &off
	input.Profile.NativeMode = "agent-full-access"
	caps := c.DriverCapabilities{ExplicitConfig: true, ToolAllowList: true}
	grants := c.Grants{Directories: []string{"/allowed"}, Tools: []string{"read"}}
	compiled, err := Compile(input, caps, grants, c.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	off = true
	if *compiled.Spec().Profile.Thinking {
		t.Fatal("accepted preference changed through caller pointer")
	}
	snapshot := compiled.Spec()
	*snapshot.Profile.Thinking = true
	if *compiled.Spec().Profile.Thinking {
		t.Fatal("getter exposed preference pointer")
	}
	changed, err := Compile(input, caps, grants, c.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if changed.ContextDigest() == compiled.ContextDigest() {
		t.Fatal("thinking change kept static context compatible")
	}
	input.Profile.Thinking = nil
	input.Profile.NativeMode = "agent"
	changed, err = Compile(input, caps, grants, c.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if changed.ContextDigest() == compiled.ContextDigest() {
		t.Fatal("native mode change kept static context compatible")
	}
}

// TestInactivityBudgetNormalization 防止独立无活动预算丢失、负数被接受或显式设置被默认值覆盖。
func TestInactivityBudgetNormalization(t *testing.T) {
	inherited, err := NormalizeLimits(c.Limits{}, DefaultLimits())
	if err != nil || inherited.InactivityTimeout <= 0 {
		t.Fatalf("missing inactivity default: %+v %v", inherited, err)
	}
	explicit, err := NormalizeLimits(c.Limits{InactivityTimeout: time.Second}, DefaultLimits())
	if err != nil || explicit.InactivityTimeout != time.Second {
		t.Fatalf("overrode explicit inactivity budget: %+v %v", explicit, err)
	}
	if _, err := NormalizeLimits(c.Limits{InactivityTimeout: -time.Second}, DefaultLimits()); err == nil {
		t.Fatal("negative inactivity timeout accepted")
	}
}

// TestSeedCodeSnapshotSharesHistoricalTextBudget 防止历史代码快照绕过新会话的有界播种。
func TestSeedCodeSnapshotSharesHistoricalTextBudget(t *testing.T) {
	input := spec()
	input.Session.Seed = []c.SeedMessage{{Role: "user", Text: "中", CodeReferences: []c.CodeReference{{Path: "main.go", Content: strings.Repeat("x", 64<<10)}}}}
	_, err := Compile(input, c.DriverCapabilities{ExplicitConfig: true, ToolAllowList: true}, c.Grants{Directories: []string{"/allowed"}, Tools: []string{"read"}}, c.Limits{})
	if err == nil {
		t.Fatal("historical code snapshot exceeded seed text budget")
	}
}
