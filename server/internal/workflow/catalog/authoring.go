// Package catalog 提供发现契约与 SDK 组合共用的工作流创作目录，不依赖编译器或存储。
package catalog

// Topic 描述一个可查询主题及其对应的原生调用。
type Topic struct {
	// ID 是 sdk get 接纳的稳定主题名。
	ID string `json:"id"`
	// Primitives 是该主题说明的实际 DSL 调用名。
	Primitives []string `json:"primitives"`
}

// Topics 返回按创作依赖排序的 SDK 主题。
func Topics() []Topic {
	return []Topic{
		{ID: "overview", Primitives: []string{"phase", "log"}},
		{ID: "worker", Primitives: []string{"worker"}},
		{ID: "human", Primitives: []string{"askUser"}},
		{ID: "parallel", Primitives: []string{"parallel"}},
		{ID: "pipeline", Primitives: []string{"pipeline"}},
		{ID: "workflow", Primitives: []string{"workflow", "startWorkflow", "joinWorkflow"}},
		{ID: "action", Primitives: []string{"action"}},
		{ID: "function", Primitives: []string{"functionTask"}},
		{ID: "schema", Primitives: []string{}},
		{ID: "saving", Primitives: []string{}},
		{ID: "budgets", Primitives: []string{}},
		{ID: "im", Primitives: []string{}},
	}
}

// TopicNames 返回 sdk get 的合法主题，不把原生调用名误当主题。
func TopicNames() []string {
	result := []string{}
	for _, topic := range Topics() {
		result = append(result, topic.ID)
	}
	return result
}

// PrimitiveTopics 返回原生调用到文档主题的准确映射。
func PrimitiveTopics() map[string]string {
	result := map[string]string{}
	for _, topic := range Topics() {
		for _, primitive := range topic.Primitives {
			result[primitive] = topic.ID
		}
	}
	return result
}

// Primitives 返回创作准备可选择的原生调用。
func Primitives() []string {
	result := []string{}
	for _, topic := range Topics() {
		result = append(result, topic.Primitives...)
	}
	return result
}

// Examples 返回可编译的完整示例目录。
func Examples() []string {
	return []string{"f0", "weekly-report", "environment-review", "environment-release"}
}

// ResourceKinds 返回创作资源查询实际支持的类别。
func ResourceKinds() []string {
	return []string{"wakers", "runtimes", "accounts", "connectors", "workflows", "projects", "functionLanguages", "scriptLanguages", "toolPolicyCapabilities", "builtinActions", "imTargets"}
}

// Features 返回需要额外资料或外部目标的创作能力。
func Features() []string {
	return []string{"schema", "files", "api-retrieval", "budgets", "im"}
}

// AuthoringTopics 根据声明的调用和能力组合主题，公共资料只出现一次。
func AuthoringTopics(primitives, features []string) []string {
	selected := map[string]bool{"overview": true}
	for _, primitive := range primitives {
		if topic, ok := PrimitiveTopics()[primitive]; ok {
			selected[topic] = true
		}
	}
	for _, feature := range features {
		switch feature {
		case "schema", "budgets":
			selected[feature] = true
		case "im":
			selected["action"] = true
			selected["im"] = true
		}
	}
	result := []string{}
	for _, topic := range Topics() {
		if selected[topic.ID] {
			result = append(result, topic.ID)
		}
	}
	return result
}
