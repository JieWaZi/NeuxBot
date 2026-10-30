package contract

import (
	"reflect"
	"testing"
)

// TestSessionDeclarationsExcludeRuntimeResources 防止静态声明重新接纳秘密和释放端口。
func TestSessionDeclarationsExcludeRuntimeResources(t *testing.T) {
	plan := reflect.TypeOf(SessionPlan{})
	target, _ := plan.FieldByName("Target")
	for _, name := range []string{"Environment", "RefreshCredentials", "Isolated", "CloseAfterRun"} {
		if _, exists := target.Type.FieldByName(name); exists {
			t.Errorf("static session target contains runtime resource %s", name)
		}
	}
	connectors, _ := plan.FieldByName("Connectors")
	for _, name := range []string{"Environment", "Headers"} {
		if _, exists := connectors.Type.Elem().FieldByName(name); exists {
			t.Errorf("static connector contains secret resource %s", name)
		}
	}
}
