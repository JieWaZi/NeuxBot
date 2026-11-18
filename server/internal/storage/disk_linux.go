package storage

import "golang.org/x/sys/unix"

// Linux 的容量计数以基本分配单位为准，较旧文件系统可能只填传输块大小。
func unixFilesystemBlockSize(space unix.Statfs_t) uint64 {
	if space.Frsize > 0 {
		return uint64(space.Frsize)
	}
	return uint64(space.Bsize)
}
