package workspace

import (
	"encoding/base64"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestImportUsesBase64StringContract(t *testing.T) {
	root := t.TempDir()
	scope, err := files.Open("desktop", root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close("desktop", scope.ID)
	bytes := []byte{0, 255, 1, 2}
	body, _ := json.Marshal(map[string]string{"scopeId": scope.ID, "name": "file.bin", "bytesBase64": base64.StdEncoding.EncodeToString(bytes)})
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("ally.auth.subject", "desktop")
	ctx.Request = httptest.NewRequest("POST", "/api/v1/workspace/import", strings.NewReader(string(body)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	op := &ImportFile{}
	if err = ginx.ParameterBinding(ctx, op, ginx.GetOperatorTypeInfo(reflect.TypeOf(op))); err != nil {
		t.Fatal(err)
	}
	if _, err = op.Output(ctx); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(filepath.Join(root, "file.bin"))
	if err != nil || string(actual) != string(bytes) {
		t.Fatalf("binary changed: %v %v", actual, err)
	}
	op.Body.Name = "invalid.bin"
	op.Body.BytesBase64 = "not-base64"
	if _, err = op.Output(ctx); err == nil {
		t.Fatal("invalid base64 accepted")
	}
}
func TestBodyLimitRejectsBeforeBinding(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/api/v1/workspace/delete", strings.NewReader(strings.Repeat("x", (2<<20)+1)))
	value, err := (&BodyLimit{}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	value.(gin.HandlerFunc)(ctx)
	if _, err = io.ReadAll(ctx.Request.Body); err == nil {
		t.Fatal("unbounded operation body")
	}
}
