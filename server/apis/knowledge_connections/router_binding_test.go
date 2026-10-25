package knowledge_connections

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	knowledgeService "github.com/JieWaZi/NeuxBot/server/services/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shrewx/ginx"
	"github.com/shrewx/ginx/pkg/conf"
)

// bindingChat 只公开真实Chat身份，query不能覆盖该项目范围。
type bindingChat struct {
	// Service 保持未使用宿主方法的真实端口形状。
	chat.Service
	// value 是本进程持久聊天查询结果。
	value models.Chat
}

func (c bindingChat) GetChatSummary(context.Context, string) (chat.ChatSummary, error) {
	return chat.ChatSummary{Chat: c.value}, nil
}

// bindingConnections 在真实HTTP路由测试中公开固定的可发现连接。
type bindingConnections struct {
	// connectionFixture 复用真实配置和凭据快照。
	connectionFixture
}

func (c bindingConnections) List(ctx context.Context) ([]models.KnowledgeConnection, error) {
	value, err := c.Get(ctx, "connection")
	return []models.KnowledgeConnection{value}, err
}

func TestKnowledgeRouterBindingProcess(t *testing.T) {
	if os.Getenv("ALLY_KNOWLEDGE_BINDING_TEST") != "1" {
		return
	}
	port, _ := strconv.Atoi(os.Getenv("ALLY_KNOWLEDGE_BINDING_PORT"))
	var receipts sync.Map
	library := core.Library{ID: "kb", Kind: "general", Name: "Remote fixture", Version: 1, StructureVersion: 1, PolicyVersion: 1}
	remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/activity"):
			if r.URL.Query().Get("after_id") != "12" || r.URL.Query().Get("limit") != "7" || r.URL.Query().Get("action") != "kb.updated" || r.URL.Query().Get("outcome") != "success" || r.URL.Query().Get("actor") != "server-user" {
				w.WriteHeader(400)
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":[{"id":11,"request_id":"edit","operation":"library.update","actor_user_id":"server-user","actor_role":"editor","action":"kb.updated","scope_type":"knowledge_base","scope_id":"kb","target_type":"knowledge_base","target_id":"kb","outcome":"success","details":{"name":"Remote"},"created_at":"2026-10-21T01:00:00Z"}],"next_cursor":11}`))
			return
		case strings.HasSuffix(r.URL.Path, "/sources/source/chunks"):
			_, _ = w.Write([]byte(`[{"id":"chunk","libraryId":"kb","sourceVersionId":"source","chunkId":"original-chunk","content":"远端真实分块","chunkType":"child","state":"pending","locator":{"kind":"line","startLine":3,"endLine":4}}]`))
			return
		case r.URL.Path == "/api/knowledge/v1/libraries":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []core.Library{library}, "total": 1, "offset": 0, "limit": 100})
			return
		case strings.HasSuffix(r.URL.Path, "/capabilities"):
			_ = json.NewEncoder(w).Encode(map[string]any{"profile": "neux-knowledge-v1", "operations": map[string]bool{"rawOnly": true, "organize": true}, "details": map[string]any{}, "profiles": []map[string]string{{"id": "profile", "name": "Server"}}})
			return
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/sources"):
			var input core.ImportSourceInput
			_ = json.NewDecoder(r.Body).Decode(&input)
			if string(input.Bytes) != "router actual raw" {
				w.WriteHeader(400)
				return
			}
			receipt := core.Receipt{RequestID: input.RequestID, LibraryID: "kb", ResourceID: "source", Operation: "source.import", State: "saved", Version: 1}
			receipts.Store(input.RequestID, receipt)
			_ = json.NewEncoder(w).Encode(receipt)
			return
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/organize"):
			var input map[string]any
			_ = json.NewDecoder(r.Body).Decode(&input)
			if input["profileId"] != "profile" {
				w.WriteHeader(400)
				return
			}
			id, _ := input["requestId"].(string)
			receipt := core.Receipt{RequestID: id, LibraryID: "kb", ResourceID: "job", JobID: "job", Operation: "job.accept", State: "accepted", Version: 1}
			receipts.Store(id, receipt)
			_ = json.NewEncoder(w).Encode(receipt)
			return
		case strings.HasSuffix(r.URL.Path, "/receipts"):
			receipt, ok := receipts.Load(r.URL.Query().Get("requestId"))
			if !ok {
				w.WriteHeader(404)
				_, _ = w.Write([]byte(`{"code":"not_found"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(receipt)
			return
		case strings.HasSuffix(r.URL.Path, "/jobs/job"):
			_ = json.NewEncoder(w).Encode(core.Job{ID: "job", LibraryID: "kb", State: "queued", Version: 1, Attempt: 1, SourceVersionIDs: []string{"source"}})
			return
		}

		if strings.HasSuffix(r.URL.Path, "/pages") {
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			if offset != 12 || limit != 7 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "total": 25, "offset": offset, "limit": limit})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "kb", "kind": "general", "name": "Remote fixture", "version": 1, "structureVersion": 1, "policyVersion": 1})
	})
	root := ginx.NewRouter(ginx.Group("/api/v1"))
	root.Register(Router)
	root.Register(knowledge.Router)
	fixture := global.KnowledgeConnectionControllerManager.(connectionFixture)
	global.KnowledgeConnectionControllerManager = bindingConnections{fixture}
	directory := t.TempDir()
	if err := workspace.EnsureExecutionWorkspace(directory, "actual", "actual-waker", nil); err != nil {
		t.Fatal(err)
	}
	global.KnowledgeService = knowledgeService.New(knowledgeService.Config{Chats: bindingChat{value: models.Chat{ID: "actual", WakerID: "actual-waker", WorkspaceDirectory: directory}}, Connections: global.KnowledgeConnectionControllerManager, References: global.KnowledgeControllerManager})
	ginx.RunServer(&conf.Server{Host: "127.0.0.1", Port: port, ExitWaitTimeout: 1, Log: &conf.Log{ToStdout: true}}, root)
}

func TestRealKnowledgeRouterBindsRemotePathAndPagination(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "-test.run=^TestKnowledgeRouterBindingProcess$")
	command.Env = append(os.Environ(), "ALLY_KNOWLEDGE_BINDING_TEST=1", fmt.Sprintf("ALLY_KNOWLEDGE_BINDING_PORT=%d", port), "TMPDIR="+t.TempDir())
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	client := http.Client{Timeout: 2 * time.Second}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(10 * time.Second)
	for {
		response, err := client.Get(base + "/health")
		if err == nil {
			_ = response.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("real ginx router did not start: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, test := range []struct {
		// path 是待验证的真实业务路由。
		path string
		// status 是HTTP绑定后的结果。
		status int
	}{{"/api/v1/knowledge/chat-libraries?chatId=actual&wakerId=forged&projectId=forged", 200}, {"/api/v1/knowledge/connections/connection/libraries/kb", 200}, {"/api/v1/knowledge/connections/connection/libraries/kb/pages?offset=12&limit=7", 200}, {"/api/v1/knowledge/connections/connection/libraries/kb/activity?afterId=12&limit=7&action=kb.updated&outcome=success&actor=server-user", 200}, {"/api/v1/knowledge/connections/connection/libraries/kb/sources/source/chunks", 200}} {
		response, err := client.Get(base + test.path)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if response.StatusCode != test.status {
			t.Fatalf("%s status=%d body=%s", test.path, response.StatusCode, data)
		}
		if strings.Contains(test.path, "/pages?") && !bytes.Contains(data, []byte(`"total":25`)) {
			t.Fatalf("lost actual pagination result %s", data)
		}
		if strings.Contains(test.path, "/activity?") && (!bytes.Contains(data, []byte(`"nextCursor":11`)) || !bytes.Contains(data, []byte(`"actorUserId":"server-user"`))) {
			t.Fatalf("lost actual activity cursor or identity: %s", data)
		}
		if strings.HasSuffix(test.path, "/chunks") && !bytes.Contains(data, []byte(`"content":"远端真实分块"`)) {
			t.Fatalf("lost remote source chunks: %s", data)
		}
	}
	file := filepath.Join(t.TempDir(), "router.md")
	if err := os.WriteFile(file, []byte("router actual raw"), 0600); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"requestId": "router-chat", "ref": map[string]string{"backend": "custom", "connectionId": "connection", "knowledgeBaseId": "kb"}, "profileId": "profile", "attachments": []map[string]string{{"sourcePath": file}}})
	response, err := client.Post(base+"/api/v1/knowledge/chat-operations/actual", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 || !bytes.Contains(data, []byte(`"state":"accepted"`)) || !bytes.Contains(data, []byte(`"jobId":"job"`)) {
		t.Fatalf("remote chat actual router %d %s", response.StatusCode, data)
	}
	response, err = client.Get(base + "/api/v1/knowledge/chat-operations/actual?requestId=router-chat")
	if err != nil {
		t.Fatal(err)
	}
	data, _ = io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 || !bytes.Contains(data, []byte(`"state":"accepted"`)) {
		t.Fatalf("remote chat read-only status %d %s", response.StatusCode, data)
	}

	response, err = client.Get(base + "/api/v1/knowledge/chat-operations/actual/requests?offset=0&limit=1")
	if err != nil {
		t.Fatal(err)
	}
	data, _ = io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 || !bytes.Contains(data, []byte(`"requestId":"router-chat"`)) || !bytes.Contains(data, []byte(`"total":1`)) {
		t.Fatalf("durable remote chat reference %d %s", response.StatusCode, data)
	}

}
