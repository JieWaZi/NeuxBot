package constants

//go:generate toolx gen enum StorageCategory

// StorageCategory 表示常规文件的磁盘计量归属。
type StorageCategory string

const (
	// StorageCategoryAppData 包含程序数据库、配置与受保护的桌面数据。
	StorageCategoryAppData StorageCategory = "app_data"
	// StorageCategoryKnowledge 包含知识原件、知识库和记忆数据。
	StorageCategoryKnowledge StorageCategory = "knowledge"
	// StorageCategoryPrograms 包含受管程序、技能和安装的插件资源。
	StorageCategoryPrograms StorageCategory = "programs"
	// StorageCategoryCache 包含已识别的桌面缓存及派生资源。
	StorageCategoryCache StorageCategory = "cache"
	// StorageCategoryAccounts 包含账号配置和原生历史数据。
	StorageCategoryAccounts StorageCategory = "accounts"
	// StorageCategoryTemporary 包含已识别的临时租约数据。
	StorageCategoryTemporary StorageCategory = "temporary"
	// StorageCategoryLogs 包含程序日志。
	StorageCategoryLogs StorageCategory = "logs"
	// StorageCategoryWorkFiles 包含伙伴、团队与工作流的受管业务文件。
	StorageCategoryWorkFiles StorageCategory = "work_files"
	// StorageCategoryConversations 包含会话私有数据和 IM 交付文件。
	StorageCategoryConversations StorageCategory = "conversations"
	// StorageCategorySharedNative 包含跨会话共享的规范原生分片。
	StorageCategorySharedNative StorageCategory = "shared_native"
	// StorageCategoryRecovery 包含跨存储操作的暂存和恢复记录。
	StorageCategoryRecovery StorageCategory = "recovery"
	// StorageCategoryOther 包含根内未核验归属的文件。
	StorageCategoryOther StorageCategory = "other"
)
