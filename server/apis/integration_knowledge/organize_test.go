package integration_knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	knowledgeservice "github.com/JieWaZi/NeuxBot/server/services/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublishedOrganizeFreezesHostAuthorityWithoutClientRuntime(t *testing.T) {
	e, lib, auth, refs := publishedFixture(t, constants.IntegrationCapabilityRetrieve, constants.IntegrationCapabilityOrganize)
	source, err := e.ImportSource(context.Background(), core.ImportSourceInput{RequestID: "seed", LibraryID: lib.ID, Filename: "seed.md", Bytes: []byte("# 原件")})
	if err != nil {
		t.Fatal(err)
	}
	service := knowledgeservice.New(knowledgeservice.Config{Engine: e, References: refs, IntegrationGuard: auth.RestoreKnowledgeGuard, ResolveModel: func(_ context.Context, in models.LibraryExecutionDefaults) (models.LibraryExecutionDefaults, error) {
		return models.LibraryExecutionDefaults{RuntimeID: "fixture", Model: "fixture"}, nil
	}})
	h := NewHandler(e, service, auth, Options{References: refs})
	body := fmt.Sprintf(`{"expected_version":%d,"structure_version":%d,"source_version_ids":[%q],"instruction":"整理正文"}`, lib.Version, lib.StructureVersion, source.ResourceID)
	var jobID string
	for i, input := range []string{body, body, strings.TrimSuffix(body, "}") + `,"runtimeId":"client-controlled"}`} {
		r := httptest.NewRequest("POST", "/api/v1/knowledge-bases/"+lib.ID+"/organize", strings.NewReader(input))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-API-Key", "fixture-only")
		r.Header.Set("Idempotency-Key", "organize-a")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := 200
		if i == 2 {
			want = 400
		}
		if w.Code != want {
			t.Fatal(i, w.Code, w.Body.String())
		}
		if i == 0 {
			var out struct {
				Receipt *core.Receipt `json:"receipt"`
			}
			if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Receipt == nil || out.Receipt.JobID == "" {
				t.Fatal(w.Body.String())
			}
			jobID = out.Receipt.JobID
		}
		if strings.Contains(w.Body.String(), "fixture") || strings.Contains(w.Body.String(), "authorization") {
			t.Fatal("private configuration leaked", w.Body.String())
		}
	}
	job, err := e.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatal(err)
	}
	config, err := refs.GetTaskConfiguration(context.Background(), job.RequestID)
	if err != nil || config.RemoteSubject != "key-a" {
		t.Fatal(config, err)
	}
	var recovery struct {
		Origin        string                          `json:"origin"`
		Authorization models.IntegrationAuthorization `json:"authorization"`
	}
	if json.Unmarshal(config.RemoteAuthorization, &recovery) != nil || recovery.Origin != "direct_api" || recovery.Authorization.Identity.CredentialGeneration != 1 {
		t.Fatal(string(config.RemoteAuthorization))
	}
	_, total, err := e.ListJobs(context.Background(), lib.ID, 0, 10)
	if err != nil || total != 1 {
		t.Fatal(total, err)
	}
}

func TestPublishedExplicitEmptyAndOmittedSources(t *testing.T) {
	for _, explicit := range []bool{true, false} {
		t.Run(fmt.Sprint(explicit), func(t *testing.T) {
			e, lib, auth, refs := publishedFixture(t, constants.IntegrationCapabilityRetrieve, constants.IntegrationCapabilityOrganize)
			_, err := e.ImportSource(context.Background(), core.ImportSourceInput{RequestID: "seed", LibraryID: lib.ID, Filename: "seed.md", Bytes: []byte("原件")})
			if err != nil {
				t.Fatal(err)
			}
			service := knowledgeservice.New(knowledgeservice.Config{Engine: e, References: refs, IntegrationGuard: auth.RestoreKnowledgeGuard, ResolveModel: func(context.Context, models.LibraryExecutionDefaults) (models.LibraryExecutionDefaults, error) {
				return models.LibraryExecutionDefaults{RuntimeID: "fixture"}, nil
			}})
			body := fmt.Sprintf(`{"expected_version":%d,"structure_version":%d}`, lib.Version, lib.StructureVersion)
			if explicit {
				body = strings.TrimSuffix(body, "}") + `,"source_version_ids":[]}`
			}
			r := httptest.NewRequest("POST", "/api/v1/knowledge-bases/"+lib.ID+"/organize", strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-API-Key", "fixture-only")
			r.Header.Set("Idempotency-Key", "organize")
			w := httptest.NewRecorder()
			NewHandler(e, service, auth, Options{References: refs}).ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			var out PublishedJobAcceptance
			if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Noop != explicit {
				t.Fatal(w.Body.String())
			}
		})
	}
}
