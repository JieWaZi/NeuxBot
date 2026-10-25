package terminalruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ginxlogx "github.com/shrewx/ginx/pkg/logx"
)

func fakeSupervisor(t *testing.T) *Supervisor {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node 不可用")
	}
	worker := filepath.Join(t.TempDir(), "worker.cjs")
	err = os.WriteFile(worker, []byte(`process.stderr.write('worker startup diagnostic\n');const readline=require('node:readline');let seq=0;let attach=0;const sessions=new Map();const owners=new Map();const send=o=>process.stdout.write(JSON.stringify(o)+'\n');send({event:'ready',payload:{protocolVersion:1}});readline.createInterface({input:process.stdin}).on('line',line=>{const {id,method,args}=JSON.parse(line);let result=null;let error;switch(method){case 'start':if(!args.createWhenMissing){error='missing';break};result={sessionId:'s'+(++seq),shell:'test-shell'};sessions.set(result.sessionId,result);owners.set(args.ownerKey,result.sessionId);break;case 'attach':if(!sessions.has(args.sessionId)){error='missing';break};result={...sessions.get(args.sessionId),attachmentId:'a'+(++attach),screen:'restored',sequence:0,state:'running'};break;case 'write':send({event:'terminal-output',payload:{sessionId:args.sessionId,sequence:1,data:args.data}});break;case 'close':sessions.delete(owners.get(args.ownerKey));owners.delete(args.ownerKey);result=true;break;case 'dispose':send({id,result:true});process.exit(0);return;}send(error?{id,error}:{id,result});});`), 0600)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(node, worker)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestTerminalDiagnosticsKeepProtocolClean(t *testing.T) {
	var logs bytes.Buffer
	logger := ginxlogx.Instance()
	original := logger.Out
	logger.SetOutput(&logs)
	t.Cleanup(func() { logger.SetOutput(original) })

	s := fakeSupervisor(t)
	if !s.Available() {
		t.Fatal("worker diagnostics prevented the JSONL handshake")
	}
	s.Close()
	if !strings.Contains(logs.String(), "worker startup diagnostic") {
		t.Fatal("worker diagnostics bypassed the unified logger")
	}
}

func TestTerminalOwnershipRefreshAndExplicitClose(t *testing.T) {
	s := fakeSupervisor(t)
	ctx := context.Background()
	started, err := s.Start(ctx, ctx, "web:a", "panel", "/allowed", true)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := s.Start(ctx, ctx, "web:a", "panel", "/allowed", false)
	if err != nil || restored != started {
		t.Fatal("did not resume", err)
	}
	if _, err = s.Start(ctx, ctx, "web:a", "panel", "/other", true); err == nil {
		t.Fatal("root changed")
	}
	if _, _, err = s.Attach(ctx, "web:b", started.SessionID); err == nil {
		t.Fatal("cross subject attachment")
	}
	snapshot, a, err := s.Attach(ctx, "web:a", started.SessionID)
	if err != nil || snapshot.Screen != "restored" {
		t.Fatal(err)
	}
	if err = s.Control(ctx, "web:b", started.SessionID, a, "write", "hello", 0, 0); err == nil {
		t.Fatal("cross subject write")
	}
	if err = s.Control(ctx, "web:a", started.SessionID, a, "resize", "", 0, 500); err == nil {
		t.Fatal("invalid size")
	}
	if err = s.Control(ctx, "web:a", started.SessionID, a, "write", strings.Repeat("x", 65537), 0, 0); err == nil {
		t.Fatal("unbounded input")
	}
	if err = s.Control(ctx, "web:a", started.SessionID, a, "write", "hello", 0, 0); err != nil {
		t.Fatal(err)
	}
	select {
	case data := <-a.Events:
		if !strings.Contains(string(data), "hello") {
			t.Fatal("output missing")
		}
		a.Consumed(len(data))
	case <-time.After(time.Second):
		t.Fatal("no output")
	}
	s.Detach("web:a", started.SessionID, a)
	_, next, err := s.Attach(ctx, "web:a", started.SessionID)
	if err != nil || a.ID == next.ID {
		t.Fatal("fresh attachment required", err)
	}
	if err = s.CloseOwner("web:a", "panel"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-next.Context.Done():
	default:
		t.Fatal("not revoked")
	}
	if _, _, err = s.Attach(ctx, "web:a", started.SessionID); err == nil {
		t.Fatal("closed session revived")
	}
}

func TestTerminalLogoutGraceAndBackpressure(t *testing.T) {
	s := fakeSupervisor(t)
	s.gracePeriod = 20 * time.Millisecond
	ctx := context.Background()
	lifetime, cancel := context.WithCancel(ctx)
	started, err := s.Start(ctx, lifetime, "web:a", "panel", "/allowed", true)
	if err != nil {
		t.Fatal(err)
	}
	_, a, err := s.Attach(ctx, "web:a", started.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-a.Context.Done():
	case <-time.After(time.Second):
		t.Fatal("logout did not revoke")
	}
	started, err = s.Start(ctx, ctx, "web:b", "panel", "/allowed", true)
	if err != nil {
		t.Fatal(err)
	}
	_, a, err = s.Attach(ctx, "web:b", started.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"sessionId": started.SessionID, "sequence": 1, "data": strings.Repeat("x", (1<<20)+1)})
	s.emit(wireMessage{Event: "terminal-output", Payload: payload})
	select {
	case <-a.Context.Done():
	default:
		t.Fatal("slow consumer not detached")
	}
	s.Detach("web:b", started.SessionID, a)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		_, exists := s.sessions[started.SessionID]
		s.mu.Unlock()
		if !exists {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("grace period did not release PTY")
}

func TestRealPTYWorker(t *testing.T) {
	worker := os.Getenv("ALLY_TERMINAL_TEST_WORKER")
	if worker == "" {
		t.Skip("设置 ALLY_TERMINAL_TEST_WORKER 运行真实 PTY 验收")
	}
	t.Setenv("SHELL", "/bin/sh")
	s, err := New("node", worker)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	started, err := s.Start(ctx, ctx, "desktop", "smoke", t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	_, attachment, err := s.Attach(ctx, "desktop", started.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Control(ctx, "desktop", started.SessionID, attachment, "resize", "", 120, 30); err != nil {
		t.Fatal(err)
	}
	if err = s.Control(ctx, "desktop", started.SessionID, attachment, "write", "printf 'NEUX_WEB_PTY_%s\\n' OK\r", 0, 0); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	seen := false
	for !seen {
		select {
		case data := <-attachment.Events:
			attachment.Consumed(len(data))
			seen = strings.Contains(string(data), "NEUX_WEB_PTY_OK")
		case <-deadline:
			t.Fatal("真实 PTY 没有输出")
		}
	}
	s.Detach("desktop", started.SessionID, attachment)
	snapshot, _, err := s.Attach(ctx, "desktop", started.SessionID)
	if err != nil || !strings.Contains(snapshot.Screen, "NEUX_WEB_PTY_OK") {
		t.Fatal("screen not restored", err)
	}
	if err = s.CloseOwner("desktop", "smoke"); err != nil {
		t.Fatal(err)
	}
}
