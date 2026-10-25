package knowledge_connections

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/remote"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRemoteRecoveryAfterUnmountUsesOriginalConnectionRevision(t *testing.T) {
	writes, receipts, wrongTarget := 0, 0, 0
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "secret" {
			t.Error("did not use original secret")
		}
		switch r.URL.Path {
		case "/api/v1/knowledge-bases/kb":
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"id": "kb", "name": "原目标", "type": "wiki"}})
		case "/api/v1/knowledge-bases/kb/integration-capabilities":
			json.NewEncoder(w).Encode(map[string]any{"service_kind": "neuxally", "protocol_version": "1", "upload_mode": "separate", "details": map[string]any{"read": remote.OperationStatus{Supported: true, Permission: "allowed", Condition: "ready"}, "rawOnly": remote.OperationStatus{Supported: true, Permission: "allowed", Condition: "ready"}}})
		case "/api/v1/knowledge-bases/kb/knowledge/file":
			writes++
			w.WriteHeader(500)
		case "/api/v1/knowledge-bases/kb/integration-receipts/upload-original":
			receipts++
			json.NewEncoder(w).Encode(core.Receipt{RequestID: "upload-original", LibraryID: "kb", ResourceID: "source", Operation: "source.import", State: "saved", Version: 1})
		default:
			t.Error("unexpected request", r.URL)
		}
	})
	db := ctx.MustGet("testReferenceDB").(*gorm.DB)
	oldConnection, _ := global.KnowledgeConnectionControllerManager.Get(ctx, "connection")
	store := controllers.NewKnowledgeConnectionController(db)
	global.KnowledgeConnectionControllerManager = store
	oldMounts := global.KnowledgeRemoteMountControllerManager
	mounts := controllers.NewKnowledgeRemoteMountController(db)
	global.KnowledgeRemoteMountControllerManager = mounts
	t.Cleanup(func() { global.KnowledgeRemoteMountControllerManager = oldMounts })
	detected := remote.Detection{BaseURL: oldConnection.BaseURL + "/api/v1", ServiceKind: "neuxally", UploadMode: "separate", Library: remote.Library{ID: "kb", Name: "原目标"}}
	mount, err := mounts.Add(ctx, controllers.AddKnowledgeRemoteMountInput{RequestID: "mount", ConnectionID: "connection", MountID: "mount", Credential: "secret", Detection: detected})
	if err != nil {
		t.Fatal(err)
	}
	input := RemoteUploadInput{ImportSourceInput: knowledge.ImportSourceInput{RequestID: "upload-original", Filename: "raw.md", Bytes: base64.StdEncoding.EncodeToString([]byte("原件"))}, ConnectionRevision: 1, UploadMode: "separate"}
	for i := 0; i < 2; i++ {
		_, err = (&UploadRemoteSource{ConnectionID: "connection", LibraryID: "kb", Body: input}).Output(ctx)
		if i == 0 && !errors.Is(err, status_error.KnowledgeRemoteOutcomeUnknown) {
			t.Fatal(err)
		}
		if i == 1 && err != nil {
			t.Fatal(err)
		}
	}
	if writes != 1 {
		t.Fatal("unknown write resent", writes)
	}
	if err = mounts.Remove(ctx, mount.ID, mount.Revision); err != nil {
		t.Fatal(err)
	}
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { wrongTarget++; http.Error(w, "wrong target", 500) }))
	defer other.Close()
	connection, err := store.Get(ctx, "connection")
	if err != nil {
		t.Fatal(err)
	}
	connection.BaseURL = other.URL + "/api/v1"
	if _, err = store.Save(ctx, controllers.SaveKnowledgeConnectionInput{RequestID: "retarget", ExpectedRevision: 1, Connection: connection, Credential: "new-secret"}); err != nil {
		t.Fatal(err)
	}
	result, err := (&GetRemoteReceipt{ConnectionID: "connection", LibraryID: "kb", RequestID: "upload-original"}).Output(ctx)
	if err != nil || result.(knowledge.Receipt).ResourceID != "source" || writes != 1 || wrongTarget != 0 || receipts < 1 {
		t.Fatal("original recovery target lost", result, err, writes, receipts, wrongTarget)
	}
	_ = models.KnowledgeRemoteMount{}
}

func TestRemoteNativeUploadFactsAndModeChange(t *testing.T) {
	for _, mode := range []string{"accepted", "unknown", "changed"} {
		t.Run(mode, func(t *testing.T) {
			writes := 0
			ctx := remoteContext(t, "weknora", func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-API-Key") != "secret" {
					t.Error("wrong credential")
				}
				switch r.URL.Path {
				case "/api/v1/knowledge-bases/kb":
					json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"id": "kb", "name": "目标", "type": "document"}})
				case "/api/v1/knowledge-bases/kb/integration-capabilities":
					if mode == "changed" {
						json.NewEncoder(w).Encode(map[string]any{"service_kind": "neuxally", "protocol_version": "1", "upload_mode": "separate", "details": map[string]any{"read": remote.OperationStatus{Supported: true, Permission: "allowed", Condition: "ready"}}})
						return
					}
					http.NotFound(w, r)
				case "/api/v1/system/info":
					json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"version": "fixture-v1", "commit_id": "8776a2d92d11ad3e76865d51cf3192e37e2a9976"}})
				case "/api/v1/knowledge-bases/kb/knowledge/file":
					writes++
					if mode == "unknown" {
						http.Error(w, "lost result", 500)
						return
					}
					if r.Method != "POST" {
						t.Error(r.Method)
					}
					file, _, err := r.FormFile("file")
					if err != nil {
						t.Fatal(err)
					}
					file.Close()
					json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"id": "source", "knowledge_base_id": "kb", "file_name": "raw.md", "parse_status": "pending"}})
				case "/api/v1/knowledge/source":
					json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"id": "source", "knowledge_base_id": "kb", "file_name": "raw.md", "parse_status": "pending"}})
				default:
					t.Errorf("unexpected request %s", r.URL)
				}
			})
			db := ctx.MustGet("testReferenceDB").(*gorm.DB)
			original, _ := global.KnowledgeConnectionControllerManager.Get(ctx, "connection")
			global.KnowledgeConnectionControllerManager = controllers.NewKnowledgeConnectionController(db)
			old := global.KnowledgeRemoteMountControllerManager
			mounts := controllers.NewKnowledgeRemoteMountController(db)
			global.KnowledgeRemoteMountControllerManager = mounts
			t.Cleanup(func() { global.KnowledgeRemoteMountControllerManager = old })
			_, err := mounts.Add(ctx, controllers.AddKnowledgeRemoteMountInput{RequestID: "mount", MountID: "mount", ConnectionID: "connection", Credential: "secret", Detection: remote.Detection{BaseURL: original.BaseURL + "/api/v1", ServiceKind: "weknora", UploadMode: "automatic", Library: remote.Library{ID: "kb", Name: "目标"}}})
			if err != nil {
				t.Fatal(err)
			}
			input := RemoteUploadInput{ImportSourceInput: knowledge.ImportSourceInput{RequestID: "native-original", Filename: "raw.md", Bytes: base64.StdEncoding.EncodeToString([]byte("原件"))}, ConnectionRevision: 1, UploadMode: "automatic"}
			out, err := (&UploadRemoteSource{ConnectionID: "connection", LibraryID: "kb", Body: input}).Output(ctx)
			if mode == "changed" {
				if !errors.Is(err, status_error.KnowledgeConflict) || writes != 0 {
					t.Fatal(out, err, writes)
				}
				return
			}
			if mode == "unknown" {
				if !errors.Is(err, status_error.KnowledgeRemoteOutcomeUnknown) {
					t.Fatal(err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				value := out.(RemoteUploadResult)
				if !value.Accepted || value.Receipt != nil || value.SourceID != "source" || value.ProcessingState != "pending" {
					t.Fatal(value)
				}
			}
			// 同一原操作重放只读取登记或远端状态，不能重复原生上传。
			_, _ = (&UploadRemoteSource{ConnectionID: "connection", LibraryID: "kb", Body: input}).Output(ctx)
			if writes != 1 {
				t.Fatal("native upload resent", writes)
			}
		})
	}
}

func TestRemoteUploadRechecksConnectionAndMountAfterDetection(t *testing.T) {
	for _, change := range []string{"revise", "disable", "unmount"} {
		t.Run(change, func(t *testing.T) {
			detecting := make(chan struct{})
			resume := make(chan struct{})
			var writes atomic.Int32
			ctx := remoteContext(t, "weknora", func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/knowledge-bases/kb":
					close(detecting)
					<-resume
					json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"id": "kb", "name": "目标", "type": "document"}})
				case "/api/v1/knowledge-bases/kb/integration-capabilities":
					http.NotFound(w, r)
				case "/api/v1/system/info":
					json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"version": "fixture-v1", "commit_id": "8776a2d92d11ad3e76865d51cf3192e37e2a9976"}})
				case "/api/v1/knowledge-bases/kb/knowledge/file":
					writes.Add(1)
					json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"id": "source", "knowledge_base_id": "kb", "parse_status": "pending"}})
				default:
					t.Errorf("unexpected request %s", r.URL)
				}
			})
			db := ctx.MustGet("testReferenceDB").(*gorm.DB)
			original, _ := global.KnowledgeConnectionControllerManager.Get(ctx, "connection")
			connections := controllers.NewKnowledgeConnectionController(db)
			global.KnowledgeConnectionControllerManager = connections
			oldMounts := global.KnowledgeRemoteMountControllerManager
			mounts := controllers.NewKnowledgeRemoteMountController(db)
			global.KnowledgeRemoteMountControllerManager = mounts
			t.Cleanup(func() { global.KnowledgeRemoteMountControllerManager = oldMounts })
			_, err := mounts.Add(ctx, controllers.AddKnowledgeRemoteMountInput{RequestID: "mount", MountID: "mount", ConnectionID: "connection", Credential: "secret", Detection: remote.Detection{BaseURL: original.BaseURL + "/api/v1", ServiceKind: "weknora", UploadMode: "automatic", Library: remote.Library{ID: "kb", Name: "目标"}}})
			if err != nil {
				t.Fatal(err)
			}
			input := RemoteUploadInput{ImportSourceInput: knowledge.ImportSourceInput{RequestID: "upload", Filename: "raw.md", Bytes: base64.StdEncoding.EncodeToString([]byte("原件"))}, ConnectionRevision: 1, UploadMode: "automatic"}
			done := make(chan error, 1)
			go func() {
				_, err := (&UploadRemoteSource{ConnectionID: "connection", LibraryID: "kb", Body: input}).Output(ctx)
				done <- err
			}()
			select {
			case <-detecting:
			case <-time.After(5 * time.Second):
				t.Fatal("detection did not start")
			}
			if change == "unmount" {
				err = mounts.Remove(context.Background(), "mount", 1)
			} else {
				connection, e := connections.Get(context.Background(), "connection")
				if e != nil {
					t.Fatal(e)
				}
				connection.Name = "新配置"
				connection.Enabled = change != "disable"
				_, err = connections.Save(context.Background(), controllers.SaveKnowledgeConnectionInput{RequestID: "change", ExpectedRevision: 1, Connection: connection})
			}
			close(resume)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("upload did not finish")
			}
			if err == nil || writes.Load() != 0 {
				t.Fatalf("stale upload sent: writes=%d err=%v", writes.Load(), err)
			}
			if _, err = global.KnowledgeControllerManager.GetOperation(ctx, "upload"); !errors.Is(err, status_error.KnowledgeNotFound) {
				t.Fatalf("rejected upload recorded as sending: %v", err)
			}
		})
	}
}
