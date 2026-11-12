package middleware

import (
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"github.com/shrewx/ginx/pkg/i18nx"
	"github.com/shrewx/ginx/pkg/statuserror"
	"net/http"
	"strings"
)

// 应用错误响应
type ApplicationErrorFormatter struct {
	// 错误身份
	Key string `json:"key" error:"key"`
	// 错误数值码
	Code int64 `json:"code"`
	// 可读原因
	Message string `json:"message"`
	// 模板参数
	Params map[string]interface{} `json:"params,omitempty"`
	// 原因来源
	MessageSource constants.ErrorMessageSource `json:"messageSource"`
}

// Match 保留显式下游 HTTP 透传，其余错误共用应用公开契约。
func (ApplicationErrorFormatter) Match(_ *gin.Context, err error) bool {
	var downstream ginx.ClientResponseError
	return !errors.As(err, &downstream)
}

// Format 丢弃私有包装，只序列化已定义身份、模板参数和显式公开原因。
func (ApplicationErrorFormatter) Format(ctx *gin.Context, response ginx.ErrorResponse) (int, string, []byte, http.Header) {
	payload := DescribeApplicationError(ctx, response.Error())
	body, _ := json.Marshal(payload)
	status := statuserror.StatusCodeFromCode(payload.Code)
	if status < 400 {
		status = http.StatusUnprocessableEntity
	}
	return status, "application/json", body, make(http.Header)
}

// DescribeApplicationError 返回可安全展示的应用错误，包括持久接纳后的局部失败。
func DescribeApplicationError(ctx *gin.Context, cause error) ApplicationErrorFormatter {
	err := status_error.PublicError(cause)
	if err == nil {
		err = status_error.InternalServerError
	}
	var public statuserror.CommonError
	_ = errors.As(err, &public)
	source := constants.ErrorMessageSourceLocalized
	message := status_error.PublicMessage(err)
	if message != "" {
		source = constants.ErrorMessageSourcePublic
	} else {
		language := "zh"
		if strings.HasPrefix(strings.ToLower(ginx.GetLang(ctx)), "en") {
			language = "en"
		}
		message = status_error.StatusError(public.Code()).WithParams(status_error.PublicParams(err)).Localize(i18nx.Instance(), language).Value()
	}
	return ApplicationErrorFormatter{public.Key(), public.Code(), message, status_error.PublicParams(err), source}
}
