package middleware

import (
	"bytes"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"io"
	"net/http"
	"reflect"
)

// IntegrationOperatorHandler 复用同一 Operator 的绑定、校验和输出，供知识与任务公开入口共同使用。
func IntegrationOperatorHandler(prototype ginx.HandleOperator) gin.HandlerFunc {
	info := ginx.GetOperatorTypeInfo(reflect.TypeOf(prototype))
	return func(ctx *gin.Context) {
		ctx.Header("Cache-Control", "no-store")
		ctx.Header("X-Content-Type-Options", "nosniff")
		instance := info.NewInstance()
		defer info.PutInstance(instance)
		operator := instance.(ginx.HandleOperator)
		body := reflect.ValueOf(instance).Elem().FieldByName("Body")
		if body.IsValid() && body.CanAddr() && ctx.Request.Method != http.MethodGet {
			ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 16<<20)
			raw, err := io.ReadAll(ctx.Request.Body)
			if err != nil {
				AbortIntegration(ctx, status_error.BadRequest)
				return
			}
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if decoder.Decode(body.Addr().Interface()) != nil {
				AbortIntegration(ctx, status_error.BadRequest)
				return
			}
			var extra any
			if decoder.Decode(&extra) != io.EOF {
				AbortIntegration(ctx, status_error.BadRequest)
				return
			}
			ctx.Request.Body = io.NopCloser(bytes.NewReader(raw))
		}
		if ginx.ParameterBinding(ctx, instance, info) != nil {
			AbortIntegration(ctx, status_error.BadRequest)
			return
		}
		if err := operator.Validate(ctx); err != nil {
			AbortIntegration(ctx, err)
			return
		}
		result, err := operator.Output(ctx)
		if err != nil {
			AbortIntegration(ctx, err)
			return
		}
		if !ctx.Writer.Written() {
			ctx.JSON(http.StatusOK, result)
		}
	}
}
