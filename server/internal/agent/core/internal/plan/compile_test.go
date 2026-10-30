package plan

import (
	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func spec() Spec {
	return Spec{Profile: c.Profile{DriverID: "fake", Subject: "a"}, Input: c.Input{Text: "first"}, Workspace: c.WorkspaceScope{Root: "/allowed/project"}, Policies: Policies{Tools: c.ToolPolicy{Mode: c.ToolsAllowList, Allow: []string{"read", "write"}}, NativeConfig: c.NativeConfigExplicitOnly, Output: c.OutputStrict}, Contributions: c.Contributions{Instructions: []c.Instruction{{Name: "profile", Text: "static"}}}}
}
func TestFreezeInputAndPermissionIntersection(t *testing.T) {
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
}
func TestRejectResumeSeedAndUnknownEnforcement(t *testing.T) {
	in := spec()
	in.Session = c.SessionDirective{Mode: c.SessionResume}
	in.Seed = []c.SeedMessage{{Role: "user", Text: "old"}}
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

// TestColdResumeUsesNativeIdentityAndKeepsPermissionValidation 防止原生恢复绕过能力及目录上界校验。
func TestColdResumeUsesNativeIdentityAndKeepsPermissionValidation(t *testing.T) {
	input := spec()
	input.Session = c.SessionDirective{Mode: c.SessionResume, NativeHandle: "persisted-native"}
	capabilities := c.DriverCapabilities{ExplicitConfig: true, ToolAllowList: true, ColdResume: true}
	grants := c.Grants{Directories: []string{"/allowed"}, Tools: []string{"read"}}
	compiled, err := Compile(input, capabilities, grants, c.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Spec().Session.NativeHandle != "persisted-native" {
		t.Fatal("native identity lost")
	}
	capabilities.ColdResume = false
	if _, err = Compile(input, capabilities, grants, c.Limits{}); err == nil {
		t.Fatal("unverified cold resume accepted")
	}
	capabilities.ColdResume = true
	input.Workspace.Root = "/outside"
	if _, err = Compile(input, capabilities, grants, c.Limits{}); err == nil {
		t.Fatal("cold resume expanded granted workspace")
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
	in.Seed = make([]c.SeedMessage, 40)
	for i := range in.Seed {
		in.Seed[i] = c.SeedMessage{Role: "user"}
	}
	in.Seed[0].Text = strings.Repeat("x", 64<<10)
	caps := c.DriverCapabilities{ExplicitConfig: true, ToolAllowList: true}
	grants := c.Grants{Directories: []string{"/allowed"}, Tools: []string{"read"}}
	if _, err := Compile(in, caps, grants, c.Limits{}); err != nil {
		t.Fatal(err)
	}
	in.Seed[1].Text = "中"
	if _, err := Compile(in, caps, grants, c.Limits{}); err == nil {
		t.Fatal("UTF-8 bytes expanded beyond seed limit")
	}
}

// TestThinkingPreferenceIsFrozen 防止可变偏好指针改写已接纳计划。
func TestThinkingPreferenceIsFrozen(t *testing.T) {
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
	input.Seed = []c.SeedMessage{{Role: "user", Text: "中", CodeReferences: []c.CodeReference{{Path: "main.go", Content: strings.Repeat("x", 64<<10)}}}}
	_, err := Compile(input, c.DriverCapabilities{ExplicitConfig: true, ToolAllowList: true}, c.Grants{Directories: []string{"/allowed"}, Tools: []string{"read"}}, c.Limits{})
	if err == nil {
		t.Fatal("historical code snapshot exceeded seed text budget")
	}
}
