package agent

import (
	"fmt"
	"strings"
)

// DeveloperInstructions composes stable host context for the native instruction port.
// User-owned workspace instruction files remain under the native client's control.
func DeveloperInstructions(plan SessionPlan) string {
	var content strings.Builder
	for _, instruction := range plan.Instructions {
		fmt.Fprintf(&content, "## %s\n\n%s\n\n", instruction.Name, instruction.Content)
	}
	return content.String()
}
