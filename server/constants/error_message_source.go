package constants

//go:generate toolx gen enum ErrorMessageSource

// ErrorMessageSource 区分应用本地化正文与显式公开的原生原因。
type ErrorMessageSource string

const (
	// ErrorMessageSourceUnspecified 只用于尚无来源标记的历史记录。
	ErrorMessageSourceUnspecified ErrorMessageSource = ""
	// ErrorMessageSourceLocalized 表示正文必须由稳定身份与安全参数按当前语言呈现。
	ErrorMessageSourceLocalized ErrorMessageSource = "localized"
	// ErrorMessageSourcePublic 表示原生协议已明确允许展示的可读原因。
	ErrorMessageSourcePublic ErrorMessageSource = "public"
)
