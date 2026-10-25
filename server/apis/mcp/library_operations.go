package mcp

import (
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// GetLibraryConnector 管理全局库资源。
type GetLibraryConnector struct {
	ginx.MethodGet
	// 连接器标识
	ConnectorID string `in:"path" name:"connectorId" validate:"required"`
}

// Path 返回全局专业资源入口。
func (*GetLibraryConnector) Path() string { return "/:connectorId" }

// Output 执行共享用例，不伪造伙伴身份。
func (o *GetLibraryConnector) Output(ctx *gin.Context) (interface{}, error) {
	return getConnector(ctx, &GetWakerConnector{ConnectorID: o.ConnectorID})
}

// CreateLibraryConnector 管理全局库资源。
type CreateLibraryConnector struct {
	ginx.MethodPost
	// 请求体
	Body ConnectorInput `in:"body"`
}

// Path 返回全局专业资源入口。
func (*CreateLibraryConnector) Path() string { return "" }

// Output 执行共享用例，不伪造伙伴身份。
func (o *CreateLibraryConnector) Output(ctx *gin.Context) (interface{}, error) {
	return createConnector(ctx, &CreateWakerConnector{Body: o.Body})
}

// UpdateLibraryConnector 管理全局库资源。
type UpdateLibraryConnector struct {
	ginx.MethodPut
	// 连接器标识
	ConnectorID string `in:"path" name:"connectorId" validate:"required"`
	// 请求体
	Body ConnectorInput `in:"body"`
}

// Path 返回全局专业资源入口。
func (*UpdateLibraryConnector) Path() string { return "/:connectorId" }

// Output 执行共享用例，不伪造伙伴身份。
func (o *UpdateLibraryConnector) Output(ctx *gin.Context) (interface{}, error) {
	return updateConnector(ctx, &UpdateWakerConnector{ConnectorID: o.ConnectorID, Body: o.Body})
}

// TestLibraryConnector 管理全局库资源。
type TestLibraryConnector struct {
	ginx.MethodPost
	// 连接器标识
	ConnectorID string `in:"path" name:"connectorId" validate:"required"`
}

// Path 返回全局专业资源入口。
func (*TestLibraryConnector) Path() string { return "/:connectorId/test" }

// Output 执行共享用例，不伪造伙伴身份。
func (o *TestLibraryConnector) Output(ctx *gin.Context) (interface{}, error) {
	return testConnector(ctx, &TestWakerConnector{ConnectorID: o.ConnectorID})
}

// DeleteLibraryConnector 管理全局库资源。
type DeleteLibraryConnector struct {
	ginx.MethodDelete
	// 连接器标识
	ConnectorID string `in:"path" name:"connectorId" validate:"required"`
	// 确认删除
	Confirmed bool `in:"query" name:"confirmed"`
	// 预期影响伙伴数
	ExpectedAffectedCount int64 `in:"query" name:"expectedAffectedCount"`
}

// Path 返回全局专业资源入口。
func (*DeleteLibraryConnector) Path() string { return "/:connectorId" }

// Output 执行共享用例，不伪造伙伴身份。
func (o *DeleteLibraryConnector) Output(ctx *gin.Context) (interface{}, error) {
	return removeConnector(ctx, &DeleteWakerConnector{ConnectorID: o.ConnectorID, Confirmed: o.Confirmed, ExpectedAffectedCount: o.ExpectedAffectedCount})
}
