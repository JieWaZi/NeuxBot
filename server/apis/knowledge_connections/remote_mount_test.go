package knowledge_connections

import (
	"context"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestRemoteMountExactLibraryWithoutListPermission(t *testing.T) {
	failureStatus := 0
	requests := 0
	listRequests := 0
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if failureStatus != 0 {
			w.WriteHeader(failureStatus)
			return
		}
		if r.Header.Get("X-API-Key") != "fixture-only" {
			t.Error("wrong credential")
		}
		switch r.URL.Path {
		case "/proxy/api/v1/knowledge-bases/a":
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"id": "a", "name": "指定远端", "type": "wiki"}})
		case "/proxy/api/v1/knowledge-bases":
			listRequests++
			http.Error(w, "forbidden", 403)
		case "/proxy/api/v1/system/info":
			http.Error(w, "forbidden", 403)
		case "/proxy/api/v1/knowledgebase/a/wiki/pages":
			json.NewEncoder(w).Encode(map[string]any{"pages": []any{}, "total": 0, "page": 1, "page_size": 1, "total_pages": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer remote.Close()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "app.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := database.DB.DB()
	defer pool.Close()
	oldConnections, oldMounts, oldEngine, oldRefs := global.KnowledgeConnectionControllerManager, global.KnowledgeRemoteMountControllerManager, global.KnowledgeEngine, global.KnowledgeControllerManager
	global.KnowledgeConnectionControllerManager = controllers.NewKnowledgeConnectionController(database.DB)
	global.KnowledgeRemoteMountControllerManager = controllers.NewKnowledgeRemoteMountController(database.DB)
	global.KnowledgeEngine = nil
	global.KnowledgeControllerManager = nil
	defer func() {
		global.KnowledgeConnectionControllerManager = oldConnections
		global.KnowledgeRemoteMountControllerManager = oldMounts
		global.KnowledgeEngine = oldEngine
		global.KnowledgeControllerManager = oldRefs
	}()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/", nil)
	input := AddRemoteLibraryInput{RequestID: "add-a", URL: remote.URL + "/proxy/api/v1/", APIKey: "fixture-only", KnowledgeBaseID: "a"}
	raw, err := (&AddRemoteLibrary{Body: input}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first := raw.(RemoteMountReceipt)
	before := requests
	raw, err = (&AddRemoteLibrary{Body: input}).Output(ctx)
	if err != nil || raw.(RemoteMountReceipt).Mount.ID != first.Mount.ID || requests != before {
		t.Fatal("replay contacted server or changed mount", err, requests, before)
	}
	raw, err = (&ListAvailableLibraries{Location: "remote"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	list := raw.(knowledge.LibraryList)
	if list.Total != 1 || len(list.Items) != 1 || list.Items[0].ID != "a" || list.Items[0].Ref.ConnectionID != first.Mount.ConnectionID || listRequests != 0 {
		t.Fatal("mount enumerated other resources", list, listRequests)
	}
	if _, err = client(context.Background(), first.Mount.ConnectionID, "b"); err == nil {
		t.Fatal("unmounted library was reachable")
	}
	mounts, err := global.KnowledgeRemoteMountControllerManager.List(ctx, first.Mount.ConnectionID)
	if err != nil || len(mounts) != 1 {
		t.Fatal(mounts, err)
	}
	for _, status := range []int{401, 403, 404, 500, 503} {
		failureStatus = status
		raw, err = (&ListAvailableLibraries{Location: "remote"}).Output(ctx)
		if err != nil {
			t.Fatalf("remote status %d blocked catalog: %v", status, err)
		}
		list = raw.(knowledge.LibraryList)
		if list.Total != 1 || len(list.Items) != 1 || !list.Items[0].Offline || list.Items[0].Name != "指定远端" || list.Items[0].MountID != first.Mount.ID {
			t.Fatalf("remote status %d lost mounted library: %+v", status, list)
		}
	}

	failureStatus = 0
	raw, err = (&ListAvailableLibraries{Location: "remote"}).Output(ctx)
	if err != nil || raw.(knowledge.LibraryList).Items[0].Offline {
		t.Fatal("recovered remote remained offline", raw, err)
	}

	remote.Close()
	raw, err = (&ListAvailableLibraries{Location: "remote"}).Output(ctx)
	if err != nil {
		t.Fatal("offline mount should remain visible", err)
	}
	list = raw.(knowledge.LibraryList)
	if len(list.Items) != 1 || !list.Items[0].Offline || list.Items[0].Name != "指定远端" {
		t.Fatal("offline mount lost its local entry", list)
	}
}
