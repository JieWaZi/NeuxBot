// Package eventsource 定义自动规则接纳的只读观察及来源能力。
package eventsource

import (
	"context"
	"strings"
)

const (
	GitHubPullRequests = "github_pull_requests"
	PullOpened         = "pull_opened"
	PullReviewed       = "pull_reviewed"
	ChecksPassed       = "checks_passed"
)

// Config 保存规则对一个来源实例的授权和筛选条件。
type Config struct {
	// Source 标识已登记的事件适配器。
	Source string `json:"source"`
	// Repository 标识此规则授权观察的一个 GitHub 仓库。
	Repository string `json:"repository"`
	// Kinds 保存可以触发工作的变化类型。
	Kinds []string `json:"kinds"`
	// Keyword 对标题执行不区分大小写的字面匹配。
	Keyword string `json:"keyword,omitempty"`
	// ExcludeDrafts 控制草稿 PR 是否参与派工。
	ExcludeDrafts bool `json:"excludeDrafts,omitempty"`
	// TrackDiscussion 将后续变化交给同一工单。
	TrackDiscussion bool `json:"trackDiscussion,omitempty"`
	// SemanticCondition 只对通过确定性条件的新增事件执行语义判断。
	SemanticCondition string `json:"semanticCondition,omitempty"`
}

// Event 保存规范化的来源事件及独立的讨论对象身份。
type Event struct {
	// ID 是同一外部变化可重放的稳定事件身份。
	ID string `json:"id"`
	// Topic 是多次变化共同关联的外部对象身份。
	Topic string `json:"topic"`
	// Kind 区分新建 PR、提交评审和检查通过。
	Kind string `json:"kind"`
	// Title 是来源提供的有界纯文本标题。
	Title string `json:"title"`
	// URL 是来源中实际对象的链接。
	URL string `json:"url"`
	// Draft 表示观察时 PR 是否仍是草稿。
	Draft bool `json:"draft,omitempty"`
}

// Valid 检查持久化接纳要求的最小来源字段。
func (e Event) Valid() bool {
	return e.ID != "" && len(e.ID) <= 256 && e.Topic != "" && len(e.Topic) <= 200 && e.URL != ""
}

// Matches 在派工前应用规则的确定性筛选。
func (c Config) Matches(e Event) bool {
	return (!c.ExcludeDrafts || !e.Draft) && strings.Contains(strings.ToLower(e.Title), strings.ToLower(c.Keyword))
}

// Adapter 读取所授权来源实例的有界观察窗口。
type Adapter interface {
	Observe(context.Context, Config) ([]Event, error)
}

// PushAdapter 先验证签名交付，再解码已授权的来源事件。
type PushAdapter interface {
	Verify(context.Context, Config, []byte, string) ([]Event, error)
}

// Definition 保存任务编辑器使用的来源声明。
type Definition struct {
	// ID 标识适配器和对应配置。
	ID string `json:"id"`
	// Mode 表示来源如何提供观察。
	Mode string `json:"mode"`
	// Fields 保存适配器支持的配置字段。
	Fields []string `json:"fields"`
	// Kinds 保存适配器支持的事件类型。
	Kinds []string `json:"kinds"`
}

// Catalog 返回新规则可以选择的来源。
func Catalog() []Definition {
	return []Definition{{ID: GitHubPullRequests, Mode: "poll", Fields: []string{"repository", "kinds", "keyword", "excludeDrafts", "semanticCondition", "trackDiscussion"}, Kinds: gitHubKinds()}}
}

// Normalize 在保存或使用规则前校验来源配置。
func Normalize(value Config) (Config, error) {
	return normalizeGitHub(value)
}
