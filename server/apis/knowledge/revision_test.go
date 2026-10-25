package knowledge

import (
	"encoding/json"
	"github.com/JieWaZi/neux-knowledge/core"
	"testing"
)

// TestRevisionRetainsSummaryForComparison 验证历史说明参与版本比较。
func TestRevisionRetainsSummaryForComparison(t *testing.T) {
	data, err := json.Marshal(revisionDTO(&core.Revision{Summary: "旧说明", Content: "旧正文"}))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot["summary"] != "旧说明" {
		t.Fatalf("revision summary missing: %s", data)
	}
}
