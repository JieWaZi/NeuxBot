package status_error

import (
	"errors"
	"github.com/shrewx/ginx/pkg/statuserror"
	"text/template"
	"text/template/parse"
)

// messageError 在保留稳定错误身份的同时携带原生接口已经给出的公开原因。
type messageError struct {
	// public 提供接口错误码与不经改写的可读原因。
	public statuserror.CommonError
	// cause 保留 errors.Is 使用的稳定错误身份。
	cause StatusError
}

// Error 保留稳定错误的诊断标识。
func (e *messageError) Error() string { return e.public.Error() }

// As 让接口层读取同一原生原因，不将包装器当作另一种错误定义。
func (e *messageError) As(target any) bool { return errors.As(e.public, target) }

// Unwrap 保留认证、限流等既有控制流程的分类判定。
func (e *messageError) Unwrap() error { return e.cause }

// WithMessage 为已定义错误附加原生公开原因，不改变错误码或本地化兜底。
func (v StatusError) WithMessage(message string) error {
	return &messageError{
		public: &statuserror.StatusErr{K: v.Key(), ErrorCode: v.Code(), Message: message},
		cause:  v,
	}
}

// PublicError 提取按错误树顺序遇到的第一个应用错误，避免次要原生原因覆盖首要失败。
func PublicError(err error) error {
	if err == nil {
		return nil
	}
	if message, ok := err.(*messageError); ok {
		return message
	}
	if public, ok := err.(statuserror.CommonError); ok {
		if definition := StatusError(public.Code()); definition.Key() == public.Key() && definition.Key() != "UNKNOWN" {
			return public
		}
	}
	switch wrapped := err.(type) {
	case interface{ Unwrap() []error }:
		for _, child := range wrapped.Unwrap() {
			if public := PublicError(child); public != nil {
				return public
			}
		}
	case interface{ Unwrap() error }:
		return PublicError(wrapped.Unwrap())
	}
	return nil
}

// PublicMessage 只返回显式标记的公开原生原因，不把本地化缓存或诊断作为正文。
func PublicMessage(err error) string {
	if message, ok := PublicError(err).(*messageError); ok {
		return message.public.Value()
	}
	return ""
}

// PublicParams 提取应用模板实际使用的简单参数，私有字段和协议载荷不进入公开契约。
func PublicParams(err error) map[string]interface{} {
	public := PublicError(err)
	detail, ok := public.(*statuserror.StatusErr)
	if !ok {
		return nil
	}
	var result map[string]interface{}
	for key, value := range detail.Params {
		referenced := false
		var inspect func([]MessageTemplateNode)
		inspect = func(nodes []MessageTemplateNode) {
			for _, node := range nodes {
				if node.Field == key || node.If == key {
					referenced = true
				}
				inspect(node.Then)
				inspect(node.Else)
			}
		}
		for _, catalog := range MessageTemplates() {
			inspect(catalog[StatusError(detail.Code())])
		}
		if !referenced {
			continue
		}
		switch value.(type) {
		case string, bool, int, int32, int64, uint, uint32, uint64, float32, float64:
			if result == nil {
				result = make(map[string]interface{})
			}
			result[key] = value
		}
	}
	return result
}

// MessageTemplateNode 是只包含文本、简单参数和条件的安全展示模板节点。
type MessageTemplateNode struct {
	// Text 是模板的静态正文。
	Text string `json:"text,omitempty"`
	// Field 是需要插值的公开参数。
	Field string `json:"field,omitempty"`
	// If 是控制分支的公开参数。
	If string `json:"if,omitempty"`
	// Then 是参数非空时的节点。
	Then []MessageTemplateNode `json:"then,omitempty"`
	// Else 是参数为空时的节点。
	Else []MessageTemplateNode `json:"else,omitempty"`
}

// MessageTemplates 复用 Go 模板解析，供公开参数筛选与前端生成使用同一规则。
func MessageTemplates() map[string]map[StatusError][]MessageTemplateNode { return messageTemplates }

var messageTemplates = func() map[string]map[StatusError][]MessageTemplateNode {
	result := map[string]map[StatusError][]MessageTemplateNode{}
	var compile func(*parse.ListNode) []MessageTemplateNode
	field := func(pipe *parse.PipeNode) string {
		if len(pipe.Cmds) != 1 || len(pipe.Cmds[0].Args) != 1 {
			panic("unsupported application error template expression")
		}
		value, ok := pipe.Cmds[0].Args[0].(*parse.FieldNode)
		if !ok || len(value.Ident) != 1 {
			panic("unsupported application error template field")
		}
		return value.Ident[0]
	}
	compile = func(list *parse.ListNode) []MessageTemplateNode {
		result := []MessageTemplateNode{}
		if list == nil {
			return result
		}
		for _, node := range list.Nodes {
			switch value := node.(type) {
			case *parse.TextNode:
				result = append(result, MessageTemplateNode{Text: string(value.Text)})
			case *parse.ActionNode:
				result = append(result, MessageTemplateNode{Field: field(value.Pipe)})
			case *parse.IfNode:
				result = append(result, MessageTemplateNode{If: field(value.Pipe), Then: compile(value.List), Else: compile(value.ElseList)})
			default:
				panic("unsupported application error template node")
			}
		}
		return result
	}
	for lang, catalog := range GetStatusErrorMap() {
		result[lang] = map[StatusError][]MessageTemplateNode{}
		for key, message := range catalog {
			parsed, err := template.New(key.Key()).Parse(message)
			if err != nil {
				panic(err)
			}
			result[lang][key] = compile(parsed.Tree.Root)
		}
	}
	return result
}()
