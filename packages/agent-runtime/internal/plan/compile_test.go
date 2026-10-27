package plan

import (
	c "github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
