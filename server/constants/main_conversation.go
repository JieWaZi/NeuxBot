package constants

const (
	// MainConversationEventTopic 是按主体订阅的公开主会话消息更新主题。
	MainConversationEventTopic = "main-conversations"
	// WorkLiveOperationPublished 标识真正已提交的伙伴公开发言。
	WorkLiveOperationPublished = "published"
	// ConversationMessagesGuide 是主会话读写能力共用的指南标识。
	ConversationMessagesGuide = "conversations.messages"
	// ConversationMessagesListOperation 读取本次工作绑定的公开主会话。
	ConversationMessagesListOperation = "conversations.messages.list"
	// ConversationMessagePostOperation 将完整正文发到本次工作的已绑定主会话。
	ConversationMessagePostOperation = "conversations.messages.post"
)
