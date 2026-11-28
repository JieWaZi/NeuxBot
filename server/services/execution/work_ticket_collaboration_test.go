package execution

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestDelegationResumeContextRetainsEveryReport(t *testing.T) {
	reports := make([]models.WorkTicketReport, 32)
	for i := range reports {
		reports[i] = models.WorkTicketReport{ID: fmt.Sprintf("%032x", i+1), TicketID: fmt.Sprintf("%032x", i+100),
			Status: constants.WorkTicketStatusCompleted, Content: strings.Repeat("成果\t<&", 1000)}
	}
	context := delegationResumeContext(reports)
	lines := strings.Split(strings.TrimSuffix(context, "\n"), "\n")
	if len(context) > 24<<10 || len(lines) != len(reports) {
		t.Fatalf("context uses %d bytes and includes %d of %d reports", len(context), len(lines), len(reports))
	}
	for i, line := range lines {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil || row["reportId"] != reports[i].ID || row["truncated"] != true {
			t.Fatalf("result index lost report %s: %s %v", reports[i].ID, line, err)
		}
	}
}
