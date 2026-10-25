package knowledge

import (
	"encoding/json"
	"github.com/JieWaZi/neux-knowledge/core"
	"strings"
	"testing"
)

func TestSourcePurgeReviewProjectsOnlyKnownMetadata(t *testing.T) {
	page := pageDTO(&core.Page{PageMetadata: core.JSON(`{"secret":"private","sourcePurge":{"needsReview":true,"unavailableSourceIds":["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"],"unavailableVersionIds":["bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"],"purgeIds":["cccccccccccccccccccccccccccccccc"],"private":"raw"}}`)})
	data, _ := json.Marshal(page)
	if page.SourcePurgeReview == nil || !page.SourcePurgeReview.NeedsReview || strings.Contains(string(data), "private") || strings.Contains(string(data), "raw") {
		t.Fatal(string(data))
	}
}
