//go:build !unix

package changes

import (
	"io/fs"
	"os"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// openRegular 使用 Root 约束解析范围，并在读取任何字节前确认仍为原普通文件。
func openRegular(root *os.Root, path string, before fs.FileInfo) (*os.File, error) {
	f, err := root.Open(path)
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
