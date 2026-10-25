package skills

import (
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// GetLibrarySkill 管理全局库资源。
type GetLibrarySkill struct {
	ginx.MethodGet
	// 技能标识
	SkillID string `in:"path" name:"skillId" validate:"required"`
}

// Path 返回全局专业资源入口。
func (*GetLibrarySkill) Path() string { return "/:skillId" }

// Output 执行共享用例，不伪造伙伴身份。
func (o *GetLibrarySkill) Output(ctx *gin.Context) (interface{}, error) {
	return getSkill(ctx, &GetWakerSkill{SkillID: o.SkillID})
}

// UpdateLibrarySkill 管理全局库资源。
type UpdateLibrarySkill struct {
	ginx.MethodPut
	// 技能标识
	SkillID string `in:"path" name:"skillId" validate:"required"`
	// 请求体
	Body UpdateWakerSkillRequest `in:"body"`
}

// Path 返回全局专业资源入口。
func (*UpdateLibrarySkill) Path() string { return "/:skillId" }

// Validate 复用相同字段校验。
func (o *UpdateLibrarySkill) Validate(ctx *gin.Context) error {
	return (&UpdateWakerSkill{Body: o.Body}).Validate(ctx)
}

// Output 执行共享用例，不伪造伙伴身份。
func (o *UpdateLibrarySkill) Output(ctx *gin.Context) (interface{}, error) {
	return updateSkill(ctx, &UpdateWakerSkill{SkillID: o.SkillID, Body: o.Body})
}

// ScanLibrarySkillImport 管理全局库资源。
type ScanLibrarySkillImport struct {
	ginx.MethodPost
	// 请求体
	Body ScanSkillImportRequest `in:"body"`
}

// Path 返回全局专业资源入口。
func (*ScanLibrarySkillImport) Path() string { return "/import/scan" }

// Output 执行共享用例，不伪造伙伴身份。
func (o *ScanLibrarySkillImport) Output(ctx *gin.Context) (interface{}, error) {
	return scanSkillImport(ctx, &ScanWakerSkillImport{Body: o.Body})
}

// ImportLibrarySkills 管理全局库资源。
type ImportLibrarySkills struct {
	ginx.MethodPost
	// 请求体
	Body ImportSkillRequest `in:"body"`
}

// Path 返回全局专业资源入口。
func (*ImportLibrarySkills) Path() string { return "/import" }

// Validate 复用相同字段校验。
func (o *ImportLibrarySkills) Validate(ctx *gin.Context) error {
	return (&ImportWakerSkill{Body: o.Body}).Validate(ctx)
}

// Output 执行共享用例，不伪造伙伴身份。
func (o *ImportLibrarySkills) Output(ctx *gin.Context) (interface{}, error) {
	return importSkills(ctx, &ImportWakerSkill{Body: o.Body})
}

// ListLibrarySkillVersions 管理全局库资源。
type ListLibrarySkillVersions struct {
	ginx.MethodGet
	// 技能标识
	SkillID string `in:"path" name:"skillId" validate:"required"`
}

// Path 返回全局专业资源入口。
func (*ListLibrarySkillVersions) Path() string { return "/:skillId/versions" }

// Output 执行共享用例，不伪造伙伴身份。
func (o *ListLibrarySkillVersions) Output(ctx *gin.Context) (interface{}, error) {
	return listSkillVersions(ctx, &ListSkillVersions{SkillID: o.SkillID})
}

// GetLibrarySkillVersion 管理全局库资源。
type GetLibrarySkillVersion struct {
	ginx.MethodGet
	// 技能标识
	SkillID string `in:"path" name:"skillId" validate:"required"`
	// 版本标识
	VersionID string `in:"path" name:"versionId" validate:"required"`
}

// Path 返回全局专业资源入口。
func (*GetLibrarySkillVersion) Path() string { return "/:skillId/versions/:versionId" }

// Output 执行共享用例，不伪造伙伴身份。
func (o *GetLibrarySkillVersion) Output(ctx *gin.Context) (interface{}, error) {
	return getSkillVersion(ctx, &GetSkillVersion{SkillID: o.SkillID, VersionID: o.VersionID})
}

// GetLibrarySkillVersionFile 管理全局库资源。
type GetLibrarySkillVersionFile struct {
	ginx.MethodGet
	// 技能标识
	SkillID string `in:"path" name:"skillId" validate:"required"`
	// 版本标识
	VersionID string `in:"path" name:"versionId" validate:"required"`
	// 文件路径
	FilePath string `in:"query" name:"path" validate:"required"`
}

// Path 返回全局专业资源入口。
func (*GetLibrarySkillVersionFile) Path() string { return "/:skillId/versions/:versionId/file" }

// Output 执行共享用例，不伪造伙伴身份。
func (o *GetLibrarySkillVersionFile) Output(ctx *gin.Context) (interface{}, error) {
	return getSkillVersionFile(ctx, &GetSkillVersionFile{SkillID: o.SkillID, VersionID: o.VersionID, FilePath: o.FilePath})
}

// CreateLibrarySkillVersion 管理全局库资源。
type CreateLibrarySkillVersion struct {
	ginx.MethodPost
	// 技能标识
	SkillID string `in:"path" name:"skillId" validate:"required"`
	// 请求体
	Body CreateSkillVersionRequest `in:"body"`
}

// Path 返回全局专业资源入口。
func (*CreateLibrarySkillVersion) Path() string { return "/:skillId/versions" }

// Validate 复用相同字段校验。
func (o *CreateLibrarySkillVersion) Validate(ctx *gin.Context) error {
	return (&CreateSkillVersion{Body: o.Body}).Validate(ctx)
}

// Output 执行共享用例，不伪造伙伴身份。
func (o *CreateLibrarySkillVersion) Output(ctx *gin.Context) (interface{}, error) {
	return createSkillVersion(ctx, &CreateSkillVersion{SkillID: o.SkillID, Body: o.Body})
}

// RestoreLibrarySkillVersion 管理全局库资源。
type RestoreLibrarySkillVersion struct {
	ginx.MethodPost
	// 技能标识
	SkillID string `in:"path" name:"skillId" validate:"required"`
	// 版本标识
	VersionID string `in:"path" name:"versionId" validate:"required"`
	// 请求体
	Body RestoreSkillVersionRequest `in:"body"`
}

// Path 返回全局专业资源入口。
func (*RestoreLibrarySkillVersion) Path() string { return "/:skillId/versions/:versionId/restore" }

// Output 执行共享用例，不伪造伙伴身份。
func (o *RestoreLibrarySkillVersion) Output(ctx *gin.Context) (interface{}, error) {
	return restoreSkillVersion(ctx, &RestoreSkillVersion{SkillID: o.SkillID, VersionID: o.VersionID, Body: o.Body})
}

// PullLibrarySkillUpdate 管理全局库资源。
type PullLibrarySkillUpdate struct {
	ginx.MethodPost
	// 技能标识
	SkillID string `in:"path" name:"skillId" validate:"required"`
	// 请求体
	Body PullSkillUpdateRequest `in:"body"`
}

// Path 返回全局专业资源入口。
func (*PullLibrarySkillUpdate) Path() string { return "/:skillId/pull" }

// Validate 复用相同字段校验。
func (o *PullLibrarySkillUpdate) Validate(ctx *gin.Context) error {
	return (&PullSkillUpdate{Body: o.Body}).Validate(ctx)
}

// Output 执行共享用例，不伪造伙伴身份。
func (o *PullLibrarySkillUpdate) Output(ctx *gin.Context) (interface{}, error) {
	return pullSkillUpdate(ctx, &PullSkillUpdate{SkillID: o.SkillID, Body: o.Body})
}

// DeleteLibrarySkill 管理全局库资源。
type DeleteLibrarySkill struct {
	ginx.MethodDelete
	// 技能标识
	SkillID string `in:"path" name:"skillId" validate:"required"`
	// 确认删除
	Confirmed bool `in:"query" name:"confirmed"`
	// 预期影响伙伴数
	ExpectedAffectedCount int64 `in:"query" name:"expectedAffectedCount"`
}

// Path 返回全局专业资源入口。
func (*DeleteLibrarySkill) Path() string { return "/:skillId" }

// Output 执行共享用例，不伪造伙伴身份。
func (o *DeleteLibrarySkill) Output(ctx *gin.Context) (interface{}, error) {
	return removeSkill(ctx, &UninstallSkill{SkillID: o.SkillID, Confirmed: o.Confirmed, ExpectedAffectedCount: o.ExpectedAffectedCount})
}
