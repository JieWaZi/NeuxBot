// Package skills 提供伙伴技能查询、导入和启停接口。
package skills

import "github.com/shrewx/ginx"

// Router 是伙伴技能接口路由。
var Router = ginx.NewRouter(ginx.Group("/wakers"))

func init() {
	Router.Register(&ListWakerSkills{})
	Router.Register(&GetWakerSkill{})
	Router.Register(&UpdateWakerSkill{})
	Router.Register(&UninstallSkill{})
	Router.Register(&ListSkillVersions{})
	Router.Register(&CreateSkillVersion{})
	Router.Register(&GetSkillVersion{})
	Router.Register(&GetSkillVersionFile{})
	Router.Register(&RestoreSkillVersion{})
	Router.Register(&PullSkillUpdate{})
	Router.Register(&SetWakerSkillEnabled{})
	Router.Register(&ScanWakerSkillImport{})
	Router.Register(&ImportWakerSkill{})
}

// LibraryRouter 提供全局资源专业操作。
var LibraryRouter = ginx.NewRouter(ginx.Group("/skills"))

func init() {
	LibraryRouter.Register(&GetLibrarySkill{})
	LibraryRouter.Register(&UpdateLibrarySkill{})
	LibraryRouter.Register(&ScanLibrarySkillImport{})
	LibraryRouter.Register(&ImportLibrarySkills{})
	LibraryRouter.Register(&ListLibrarySkillVersions{})
	LibraryRouter.Register(&GetLibrarySkillVersion{})
	LibraryRouter.Register(&GetLibrarySkillVersionFile{})
	LibraryRouter.Register(&CreateLibrarySkillVersion{})
	LibraryRouter.Register(&RestoreLibrarySkillVersion{})
	LibraryRouter.Register(&PullLibrarySkillUpdate{})
	LibraryRouter.Register(&DeleteLibrarySkill{})
	LibraryRouter.Register(&ListLibrarySkills{})
	LibraryRouter.Register(&InstallLibrarySkill{})
}
