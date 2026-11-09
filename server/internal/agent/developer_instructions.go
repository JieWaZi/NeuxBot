package agent

import (
	"fmt"
	"strings"
)

// DeveloperInstructions composes stable host context for the native instruction port.
// User-owned workspace instruction files remain under the native client's control.
func DeveloperInstructions(instructions []Instruction) string {
	var content strings.Builder
	for _, instruction := range instructions {
		fmt.Fprintf(&content, "## %s\n\n%s\n\n", instruction.Name, instruction.Content)
	}
	return content.String()
}
