// Package constants 定义 NeuxBot 跨层共享的稳定枚举和状态常量。
package constants

//go:generate toolx gen enum SkillSource

// SkillSource 表示技能包的导入来源。
type SkillSource string

const (
	// SkillSourceUnspecified 表示未指定来源。
	SkillSourceUnspecified SkillSource = ""
	// SkillSourceBuiltin 表示随 NeuxBot 提供的内置技能。
	SkillSourceBuiltin SkillSource = "builtin"
	// SkillSourceGit 表示从 Git 地址导入的技能。
	SkillSourceGit SkillSource = "git"
	// SkillSourceZip 表示从 ZIP 压缩包导入的技能。
	SkillSourceZip SkillSource = "zip"
)
