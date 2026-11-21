package automatic_tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
	workflowinput "github.com/JieWaZi/NeuxBot/server/internal/workflow/input"
)

func TestTaskFileUploadRunResultAndDownload(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	source := `export const meta={sdk:'bot.workflow',title:'文件交付',description:'读取文件并交付真实产物',inputSchema:{type:'object',required:['document'],properties:{document:{type:'string',format:'file'}}},outputSchema:{type:'object',required:['report'],properties:{report:{type:'string',format:'file'}}},phases:[{id:'deliver',title:'交付',detail:'交付文件',canExit:true}]};phase('deliver');return {report:args.document};`
	_, config := workflowTask(t, h, source)
	config.WorkflowInputBindings = map[string]workflowinput.Binding{"document": {Source: constants.WorkflowInputSourceAPI}}
	saved, err := h.service.SaveConfiguration(ctx, h.wakerID, "", 0, config, true)
	if err != nil {
		t.Fatal(err)
	}
	base := "/v1/automatic-tasks/" + saved.TaskID
	content := []byte{0x50, 0x4b, 0xff, 0, 1}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "预算.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, base+"/files", &body)
	request.Host = "127.0.0.1"
	request.Header.Set("Authorization", "Bearer "+saved.Token)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	h.api.localHandler(constants.AutomaticTaskDefaultAPIIP).ServeHTTP(recorder, request)
	var upload workflowfiles.Upload
	if recorder.Code != http.StatusCreated || json.Unmarshal(recorder.Body.Bytes(), &upload) != nil || upload.Reference == "" {
		t.Fatalf("upload: %d %s", recorder.Code, recorder.Body.String())
	}
	payload, _ := json.Marshal(map[string]any{"args": map[string]any{"document": upload.Reference}})
	trigger := callLocal(&h.api, http.MethodPost, base+"/runs", saved.Token, "file-run", string(payload))
	var accepted PublicRun
	if trigger.Code != http.StatusAccepted || json.Unmarshal(trigger.Body.Bytes(), &accepted) != nil {
		t.Fatalf("trigger: %d %s", trigger.Code, trigger.Body.String())
	}
	pending := callLocal(&h.api, http.MethodGet, base+"/runs/"+accepted.RunID+"/result", saved.Token, "", "")
	var result PublicWorkflowResult
	if pending.Code != http.StatusOK || json.Unmarshal(pending.Body.Bytes(), &result) != nil || result.Result != nil || len(result.Files) != 0 {
		t.Fatalf("pending result: %s", pending.Body.String())
	}
	run, err := h.api.config.Tasks.GetRun(ctx, accepted.RunID)
	if err != nil {
		t.Fatal(err)
	}
	h.runWorkflowUntil(t, ctx, run.WorkflowRunID, constants.WorkflowRunStatusSucceeded)
	response := callLocal(&h.api, http.MethodGet, base+"/runs/"+accepted.RunID+"/result", saved.Token, "", "")
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.State != constants.AutomaticRunStateCompleted || len(result.Files) != 1 || !result.Files[0].Available {
		t.Fatalf("completed result: %d %s", response.Code, response.Body.String())
	}
	path := result.Result.(map[string]any)["report"].(string)
	if !filepath.IsAbs(path) || path == upload.Reference {
		t.Fatal("input was not materialized")
	}
	downloadURL := base + "/runs/" + accepted.RunID + "/files/" + result.Files[0].ID
	assertDownload := func() {
		t.Helper()
		download := callLocal(&h.api, http.MethodGet, downloadURL, saved.Token, "", "")
		if download.Code != http.StatusOK || !bytes.Equal(download.Body.Bytes(), content) || download.Header().Get("Content-Disposition") == "" {
			t.Fatalf("download bytes: %d %v", download.Code, download.Body.Bytes())
		}
	}
	assertDownload()
	if denied := callLocal(&h.api, http.MethodGet, downloadURL, "bad-token", "", ""); denied.Code != http.StatusUnauthorized {
		t.Fatalf("token bypass: %d", denied.Code)
	}
	other, err := h.service.SaveConfiguration(ctx, h.wakerID, "", 0, config, true)
	if err != nil {
		t.Fatal(err)
	}
	if cross := callLocal(&h.api, http.MethodGet, "/v1/automatic-tasks/"+other.TaskID+"/runs/"+accepted.RunID+"/result", other.Token, "", ""); cross.Code != http.StatusNotFound {
		t.Fatalf("cross-task result: %d", cross.Code)
	}
	if _, err := h.api.TriggerAPI(ctx, other.TaskID, other.Token, "cross-file", "", map[string]any{"document": upload.Reference}); err == nil {
		t.Fatal("cross-task upload accepted")
	}
	if _, err := h.api.TriggerAPI(ctx, saved.TaskID, saved.Token, "local-path", "", map[string]any{"document": path}); err == nil {
		t.Fatal("API accepted raw local path")
	}
	if err := os.WriteFile(path, []byte("changed after run"), 0600); err != nil {
		t.Fatal(err)
	}
	assertDownload()
	replay := callLocal(&h.api, http.MethodPost, base+"/runs", saved.Token, "file-run", string(payload))
	if replay.Code != http.StatusAccepted {
		t.Fatalf("replay: %s", replay.Body.String())
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != "changed after run" {
		t.Fatal("idempotent replay rewrote frozen input")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	assertDownload()
	// 业务原件即使重新出现，也不能替代本次确认的成果收据。
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	workflowRun, err := h.api.config.WorkflowRuns.GetRun(ctx, run.WorkflowRunID)
	if err != nil {
		t.Fatal(err)
	}
	receipt, found := workflowRun.OutputFiles["/report"]
	if !found || receipt.Path == "" || receipt.Digest == "" {
		t.Fatalf("missing output receipt: %+v", workflowRun.OutputFiles)
	}
	snapshot := filepath.Join(h.api.config.ManagedFilesDirectory, constants.WorkflowFileDirectoryName, workflowRun.DefinitionID, "chat-results", workflowRun.ID, receipt.Path)
	if err := os.WriteFile(snapshot, []byte("corrupted snapshot"), 0600); err != nil {
		t.Fatal(err)
	}
	corrupted := callLocal(&h.api, http.MethodGet, downloadURL, saved.Token, "", "")
	var failure map[string]string
	if corrupted.Code != status_error.WorkflowFileInvalid.StatusCode() || json.Unmarshal(corrupted.Body.Bytes(), &failure) != nil || failure["errorCode"] != status_error.WorkflowFileInvalid.Key() {
		t.Fatalf("corrupted snapshot: %d %s", corrupted.Code, corrupted.Body.String())
	}
	if err := os.Remove(snapshot); err != nil {
		t.Fatal(err)
	}
	missing := callLocal(&h.api, http.MethodGet, downloadURL, saved.Token, "", "")
	if missing.Code != http.StatusNotFound || json.Unmarshal(missing.Body.Bytes(), &failure) != nil || failure["errorCode"] != status_error.WorkflowFileNotFound.Key() {
		t.Fatalf("missing snapshot: %d %s", missing.Code, missing.Body.String())
	}
	response = callLocal(&h.api, http.MethodGet, base+"/runs/"+accepted.RunID+"/result", saved.Token, "", "")
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || len(result.Files) != 1 || result.Files[0].Available {
		t.Fatalf("missing snapshot metadata: %d %s", response.Code, response.Body.String())
	}
}
