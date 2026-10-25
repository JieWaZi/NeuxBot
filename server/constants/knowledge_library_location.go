package constants

//go:generate toolx gen enum KnowledgeLibraryLocation

// KnowledgeLibraryLocation 表示知识库目录按保存位置筛选的范围。
type KnowledgeLibraryLocation string

const (
	// KnowledgeLibraryLocationDefault 缺省时不限制保存位置。
	KnowledgeLibraryLocationDefault KnowledgeLibraryLocation = ""
	// KnowledgeLibraryLocationAll 包含本地及全部启用的远端连接。
	KnowledgeLibraryLocationAll KnowledgeLibraryLocation = "all"
	// KnowledgeLibraryLocationLocal 只读取本地知识库。
	KnowledgeLibraryLocationLocal KnowledgeLibraryLocation = "local"
	// KnowledgeLibraryLocationRemote 只读取启用连接中的远端知识库。
	KnowledgeLibraryLocationRemote KnowledgeLibraryLocation = "remote"
)
