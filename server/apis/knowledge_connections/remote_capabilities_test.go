package knowledge_connections

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestCustomCapabilitiesPreserveSupportPermissionAndCondition(t *testing.T) {
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/knowledge/v1/libraries/kb/capabilities" {
			t.Error(r.URL)
		}
		_, _ = w.Write([]byte(`{"profile":"neux-knowledge-v1","operations":{"write":false,"organize":false,"read":true},"profiles":[{"id":"server","name":"服务器"}],"details":{"write":{"supported":true,"permission":"denied","condition":"ready"},"organize":{"supported":true,"permission":"allowed","condition":"profile_unavailable"},"read":{"supported":true,"permission":"allowed","condition":"ready"}}}`))
	})
	out, err := (&GetRemoteCapabilities{ConnectionID: "connection", LibraryID: "kb"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	v := out.(RemoteCapabilities)
	if v.Permissions["write"] != "denied" || v.Permissions["organize"] != "allowed" || v.Reasons["organize"] != "profile_unavailable" {
		t.Fatalf("capability facts were replaced: %+v", v)
	}
	raw, _ := json.Marshal(v)
	var decoded struct {
		// Details 是调用方实际收到的能力明细。
		Details map[string]map[string]any `json:"details"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded.Details["write"]["supported"] != true || decoded.Details["write"]["permission"] != "denied" || decoded.Details["organize"]["condition"] != "profile_unavailable" {
		t.Fatalf("details were lost: %s, %v", raw, err)
	}
	if v.Operations["write"] || !v.Operations["read"] || len(v.Profiles) != 1 || v.Ref.ConnectionID != "connection" {
		t.Fatal(v)
	}
}

func TestOfficialCapabilitiesExposeReadAndRejectUnsupportedActions(t *testing.T) {
	ctx := remoteContext(t, "weknora", func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected probe") })
	out, err := (&GetRemoteCapabilities{ConnectionID: "connection", LibraryID: "kb"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	v := out.(RemoteCapabilities)
	for _, key := range []string{"themes", "sourceRead", "sourceDownload", "graph"} {
		if !v.Operations[key] || v.Permissions[key] != "unknown" {
			t.Errorf("read capability %s missing: %+v", key, v)
		}
	}
	for _, key := range []string{"themeWrite", "rebuild", "sourcePurge", "libraryDelete", "activity", "sourceContent"} {
		if v.Operations[key] || v.Permissions[key] != "unknown" || v.Reasons[key] != "official_profile_has_no_verified_custom_contract" {
			t.Errorf("unsupported capability %s was ambiguous: %+v", key, v)
		}
	}
}
