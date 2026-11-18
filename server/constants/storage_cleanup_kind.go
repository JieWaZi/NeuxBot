package constants

//go:generate toolx gen enum StorageCleanupKind

// StorageCleanupKind 是存储页可确认的受管清理类别。
type StorageCleanupKind string

const (
	// StorageCleanupKindCaches 清理可重建的缓存。
	StorageCleanupKindCaches StorageCleanupKind = "caches"
	// StorageCleanupKindLogs 清理不再写入的诊断日志。
	StorageCleanupKindLogs StorageCleanupKind = "logs"
	// StorageCleanupKindRuntimes 回收无引用的受管程序版本。
	StorageCleanupKindRuntimes StorageCleanupKind = "runtimes"
)
