//go:build unix

package changes

import (
	"io/fs"
	"os"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"golang.org/x/sys/unix"
)

// openRegular 在打开时拒绝链接和阻塞设备，并在读取前核对原 inode。
func openRegular(root *os.Root, path string, before fs.FileInfo) (*os.File, error) {
	f, err := root.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) {
		f.Close()
		return nil, status_error.WorkspaceResourceConflict
	}
	return f, nil
}
