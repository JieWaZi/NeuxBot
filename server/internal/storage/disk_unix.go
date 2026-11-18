//go:build darwin || linux

package storage

import (
	"fmt"
	"io/fs"
	"math"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// 保持普通文件在竞争中被替换为 FIFO 时不会阻塞打开。
const observeFileFlags = unix.O_NOFOLLOW | unix.O_NONBLOCK

func measureVolume(path string) (Disk, error) {
	var stat unix.Stat_t
	if err := unix.Stat(path, &stat); err != nil {
		return Disk{}, err
	}
	var space unix.Statfs_t
	if err := unix.Statfs(path, &space); err != nil {
		return Disk{}, err
	}
	size := unixFilesystemBlockSize(space)
	if size == 0 || uint64(space.Blocks) > math.MaxUint64/size {
		return Disk{}, fs.ErrInvalid
	}
	total := uint64(space.Blocks) * size
	return Disk{
		ID:             fmt.Sprintf("%x:%08x%08x", stat.Dev, uint32(space.Fsid.Val[0]), uint32(space.Fsid.Val[1])),
		TotalBytes:     total,
		AvailableBytes: min(uint64(space.Blocks), uint64(space.Bavail)) * size,
	}, nil
}

func filesystemIdentity(_ *os.File, info fs.FileInfo) (fileIdentity, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileIdentity{}, fs.ErrInvalid
	}
	return fileIdentity{volume: uint64(stat.Dev), inode: uint64(stat.Ino)}, nil
}

func regularFileIdentity(_ *os.Root, _ string, info fs.FileInfo) (fileIdentity, error) {
	return filesystemIdentity(nil, info)
}
