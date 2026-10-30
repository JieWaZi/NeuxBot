package contract_test

import (
	"bytes"
	"encoding/json"
	"errors"
	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	"testing"
)

type credentialFieldError struct {
	Token string // Token 模拟不能进入结果 JSON 的底层凭据字段。
}

func (e *credentialFieldError) Error() string { return e.Token }

type credentialJSONError struct {
	Token string // Token 模拟自定义序列化的底层秘密。
}

func (e *credentialJSONError) Error() string { return e.Token }
func (e *credentialJSONError) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"credential": e.Token})
}

func TestFaultCauseExcludedFromResultJSONButRemainsUnwrapped(t *testing.T) {
	const secret = "test-secret-credential"
	for _, cause := range []error{&credentialFieldError{Token: secret}, &credentialJSONError{Token: secret}} {
		t.Run(tName(cause), func(t *testing.T) {
			f := &c.Fault{Code: c.FaultPreparation, Stage: "credential", Message: "credential acquisition failed", Cause: cause}
			result := c.Result{Status: c.StatusFailed, Fault: f, DeliveryFault: &c.Fault{Code: c.FaultDelivery, Stage: "observer", Message: "delivery failed", Cause: cause}, CleanupFault: &c.Fault{Code: c.FaultCleanup, Stage: "cleanup", Message: "cleanup failed", Cause: cause}}
			data, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(data, []byte(secret)) || bytes.Contains(data, []byte("\"Cause\"")) {
				t.Fatalf("result serialized private cause: %s", data)
			}
			var decoded c.Result
			if err = json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Fault == nil || decoded.Fault.Code != c.FaultPreparation || decoded.Fault.Stage != "credential" || decoded.Fault.Message != "credential acquisition failed" {
				t.Fatalf("safe fault metadata lost: %#v", decoded.Fault)
			}
			if !errors.Is(f, cause) {
				t.Fatal("JSON exclusion broke root-cause unwrapping")
			}
		})
	}
}
func tName(cause error) string {
	switch cause.(type) {
	case *credentialFieldError:
		return "exported field"
	default:
		return "custom JSON"
	}
}
