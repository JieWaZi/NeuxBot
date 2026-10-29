package model

import (
	catalog "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/catalog"

	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"
	"github.com/JieWaZi/neux-knowledge/core"
)

// TestRealWikiMaintenanceDirectoryPagination 核验真实 CLI 分页读取临时大正文库；只授予读取，不访问生产数据。
func TestRealWikiMaintenanceDirectoryPagination(t *testing.T) {
	if os.Getenv("ALLY_REAL_WIKI_DIRECTORY") != "1" {
		t.Skip("requires explicit real-model directory transport check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	engine, err := core.Open(ctx, core.Options{DBPath: filepath.Join(t.TempDir(), "knowledge.db"), RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	receipt, err := engine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "fixture", Name: "Synthetic directory transport", WikiLanguage: "en"})
	if err != nil {
		t.Fatal(err)
	}
	libraryID := receipt.ResourceID
	for i := 0; i < 21; i++ {
		lib, err := engine.GetLibrary(ctx, libraryID)
		if err != nil {
			t.Fatal(err)
		}
		_, err = engine.WritePage(ctx, core.WritePageInput{RequestID: fmt.Sprintf("page-%d", i), LibraryID: libraryID, StructureVersion: lib.StructureVersion, Page: core.Page{Slug: fmt.Sprintf("concept/synthetic-%d", i), Title: fmt.Sprintf("Synthetic %d", i), Summary: "Synthetic test page", PageType: "concept", Status: "published", Content: "DIRECTORY_BODY_OK\n" + strings.Repeat("Synthetic content. ", 600)}})
		if err != nil {
			t.Fatal(err)
		}
	}
	ref := bridge.LocalRef(libraryID)
	reads := bridge.ReadBindings(engine, bridge.FreezeScope("", []bridge.ResourceRef{ref}), func(context.Context, string) ([]bridge.ResourceRef, error) { return []bridge.ResourceRef{ref}, nil })
	var listed, bodies atomic.Int32
	var bindings []agentcapability.Binding
	for _, binding := range reads {
		if binding.Operation != "knowledge.pages.list" && binding.Operation != "knowledge.pages.get" {
			continue
		}
		call := binding.Call
		op := binding.Operation
		binding.Call = func(ctx context.Context, r agentcapability.Request) (agentcapability.Result, error) {
			result, err := call(ctx, r)
			if err == nil {
				if op == "knowledge.pages.list" {
					listed.Add(int32(len(result.Data.(map[string]any)["items"].([]core.PageDirectoryEntry))))
				} else {
					bodies.Add(1)
				}
			}
			return result, err
		}
		bindings = append(bindings, binding)
	}
	sdk, err := catalog.New(catalog.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close(context.Background())
	detection := sdk.Detect(ctx, runtimecontract.DetectRequest{RuntimeID: runtimecontract.RuntimeIDWorkBuddy, BaseEnvironment: os.Environ()})
	if detection.State != runtimecontract.DetectionStateReady {
		t.Fatal(detection.Diagnostics)
	}
	registry := agentcapability.New(nil)
	defer registry.Close()
	endpoint := httptest.NewServer(registry)
	defer endpoint.Close()
	host := maintenanceTransportHost(runtimecontract.RuntimeIDWorkBuddy, detection, sdk)
	defer host.Close(context.Background())
	runner := NewMaintenanceRunner(MaintenanceConfig{Runtime: host, Registry: registry, Binary: os.Getenv(constants.AgentCLIBinaryEnvironment), Endpoint: endpoint.URL + constants.AgentCLIPath, RuntimeID: runtimecontract.RuntimeIDWorkBuddy, Model: "deepseek-v4.1-flash", Reasoning: "low"})
	report, err := runner.Run(ctx, MaintenanceRequest{JobID: "directory-transport", Bindings: bindings, Validate: func(context.Context) error { return nil }, Instructions: "This is a read-only synthetic transport check. Use only advertised knowledge commands. Do not inspect files outside the selected skill or attempt edits.", Message: "Read the selected ally skill. Use capabilities and describe for exact inputs. List ALL pages of library " + libraryID + " using knowledge pages list --limit 10 and the returned nextOffset until hasMore is false. Then read ONE listed page with knowledge pages get, and report its first line marker and the total number of listed pages. Do not read all bodies or use Python, shell pipelines, files or external services."})
	if err != nil || listed.Load() < 21 || bodies.Load() < 1 || !strings.Contains(report, "DIRECTORY_BODY_OK") {
		t.Fatalf("directory transport failed: listed=%d bodies=%d err=%v report=%q", listed.Load(), bodies.Load(), err, report)
	}
	t.Logf("real paginated directory + separate body passed: listed=%d bodies=%d", listed.Load(), bodies.Load())
}
