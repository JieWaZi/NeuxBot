package agent

import (
	"testing"
)

func TestAdditionalDirectorySupportKeepsUnknownAndVerifiedLaunchEntrypoints(t *testing.T) {
	for _, test := range []struct {
		// name identifies the negotiated or launch capability case.
		name string
		// detection is the frozen runtime evidence used at admission.
		detection DetectionResult
		// supported is whether the existing native entry can carry directories.
		supported bool
		// known distinguishes explicit refusal from missing discovery evidence.
		known bool
	}{
		{name: "native", detection: DetectionResult{Capabilities: CapabilitySet{CapabilityAddDirectory: true}}, supported: true, known: true},
		{name: "unsupported", detection: DetectionResult{Capabilities: CapabilitySet{CapabilityAddDirectory: false}}, known: true},
		{name: "unknown", detection: DetectionResult{}, known: false},
		{name: "verified-launch", detection: DetectionResult{Runtime: RuntimeSummary{BaseRuntimeID: RuntimeIDWorkBuddy}, Capabilities: CapabilitySet{CapabilityAddDirectory: false}}, supported: true, known: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			supported, known := AdditionalDirectorySupport(test.detection)
			if supported != test.supported || known != test.known {
				t.Fatalf("support=%v known=%v", supported, known)
			}
		})
	}
}
