//go:build !windows

package capability

import "os"

func privateFile(info os.FileInfo) bool             { return info.Mode().Perm()&0o077 == 0 }
func protectDirectory(path string) error            { return os.Chmod(path, 0o700) }
func replaceConnection(source, target string) error { return os.Rename(source, target) }
