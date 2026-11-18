package storage

import "golang.org/x/sys/unix"

// Darwin 的容量计数以文件系统块大小为准，Iosize 是优先传输大小。
func unixFilesystemBlockSize(space unix.Statfs_t) uint64 {
	return uint64(space.Bsize)
}
