package constants

//go:generate toolx gen enum ReadingIntent

// LocalReaderID 是 Core 为本机用户固定使用的阅读身份。
const LocalReaderID = "00000000000000000000000000000001"

// ReadingIntent 表示推进会话阅读边界的用户意图。
type ReadingIntent string

const (
	// ReadingIntentAutomatic 表示公开消息实际呈现后自动读取。
	ReadingIntentAutomatic ReadingIntent = "automatic"
	// ReadingIntentExplicit 表示用户主动打开会话或标为已读。
	ReadingIntentExplicit ReadingIntent = "explicit"
)
