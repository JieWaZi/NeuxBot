package constants

//go:generate toolx gen enum ResourceCategoryType

// ResourceCategoryType 区分用户分类所属的资源类型。
type ResourceCategoryType string

const (
	// ResourceCategoryTypeUnspecified 表示未指定资源类型。
	ResourceCategoryTypeUnspecified ResourceCategoryType = ""
	// ResourceCategoryTypePlugin 表示插件分类。
	ResourceCategoryTypePlugin ResourceCategoryType = "plugin"
	// ResourceCategoryTypeSkill 表示独立技能分类。
	ResourceCategoryTypeSkill ResourceCategoryType = "skill"
	// ResourceCategoryTypeConnector 表示独立连接器分类。
	ResourceCategoryTypeConnector ResourceCategoryType = "connector"
)
