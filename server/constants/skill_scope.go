// Package constants 定义 NeuxBot 跨层共享的稳定枚举和状态常量。
package constants

//go:generate toolx gen enum SkillScope

// SkillScope 表示技能包对伙伴的可见范围。
type SkillScope string

const (
	// SkillScopeUnspecified 表示数据库或框架产生的空范围。
	SkillScopeUnspecified SkillScope = ""
	// SkillScopePublic 表示所有伙伴都可见的公开技能。
	SkillScopePublic SkillScope = "public"
	// SkillScopePrivate 表示只属于单个伙伴的私有技能。
	SkillScopePrivate SkillScope = "private"
)
