package middleware

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/i18nx"
	"net/http/httptest"
	"testing"
)

type presentationResponse struct {
	// ErrorResponse 提供本测试不消费的框架响应方法。
	ginx.ErrorResponse
	// cause 是响应正在报告的真实失败。
	cause error
}

func (r presentationResponse) Error() error { return r.cause }

func TestErrorPresentationSeparatesLocalizedAndPublicMessages(t *testing.T) {
	i18nx.Load(&conf.I18N{Langs: []string{"zh", "en"}})
	for _, tt := range []struct {
		// name 是公开响应场景。
		name string
		// failure 是真实应用错误或私有诊断。
		failure error
		// key 是期望稳定身份。
		key string
		// source 是期望消息来源。
		source string
		// message 是显式公开原因。
		message string
	}{
		{"wrapped", fmt.Errorf("private context: %w", status_error.RuntimeWorkspaceScopeUnsupported.WithParams(map[string]interface{}{"MaxDirectories": 2, "Credential": "private"})), "RuntimeWorkspaceScopeUnsupported", "localized", ""},
		{"joined", errors.Join(status_error.WorkspacePathInvalid, status_error.RuntimeExecutionFailed.WithMessage("secondary native reason")), "WorkspacePathInvalid", "localized", ""},
		{"native", status_error.RuntimeQuotaExhausted.WithMessage("Native quota has been exhausted"), "RuntimeQuotaExhausted", "public", "Native quota has been exhausted"},
		{"unknown", errors.New("private implementation detail"), "InternalServerError", "localized", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(nil)
			ctx.Request = httptest.NewRequest("GET", "/", nil)
			status, _, body, _ := (ApplicationErrorFormatter{}).Format(ctx, presentationResponse{cause: tt.failure})
			var data map[string]interface{}
			if err := json.Unmarshal(body, &data); err != nil {
				t.Fatal(err)
			}
			if status < 400 || data["key"] != tt.key || data["messageSource"] != tt.source || data["message"] == "" {
				t.Fatalf("response = %s", body)
			}
			if tt.message != "" && data["message"] != tt.message {
				t.Fatalf("native reason = %s", body)
			}
			if tt.name == "wrapped" {
				params, _ := data["params"].(map[string]interface{})
				if params["MaxDirectories"] != float64(2) || params["Credential"] != nil {
					t.Fatalf("parameters = %s", body)
				}
			}
		})
	}
}

func TestHTTPApplicationFailureKeepsLanguageAndSafeParameters(t *testing.T) {
	i18nx.Load(&conf.I18N{Langs: []string{"zh", "en"}})
	for language, want := range map[string]string{"zh": "当前智能体最多支持 2 个项目目录，请调整工作范围", "zh-CN": "当前智能体最多支持 2 个项目目录，请调整工作范围", "en": "the selected agent supports up to 2 project directories; adjust the workspace scope", "en-US": "the selected agent supports up to 2 project directories; adjust the workspace scope"} {
		ctx, _ := gin.CreateTestContext(nil)
		ctx.Request = httptest.NewRequest("GET", "/", nil)
		ctx.Request.Header.Set(ginx.CurrentLangHeader(), language)
		_, _, body, _ := (ApplicationErrorFormatter{}).Format(ctx, presentationResponse{cause: status_error.RuntimeWorkspaceScopeUnsupported.WithParams(map[string]interface{}{"MaxDirectories": 2, "Credential": "private"})})
		var data map[string]interface{}
		if err := json.Unmarshal(body, &data); err != nil {
			t.Fatal(err)
		}
		if data["message"] != want || data["messageSource"] != "localized" {
			t.Fatalf("language=%s response=%s", language, body)
		}
	}
}
