package packagefiles

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const maxResourcePreviewBytes = 1024 * 1024

// 资源条目
type ResourceEntry struct {
	// 资源路径
	Path string `json:"path"`
	// 是否目录
	Directory bool `json:"directory"`
	// 字节数
	Size int64 `json:"size,omitempty"`
	// 内容摘要
	Digest string `json:"digest,omitempty"`
	// 可执行权限
	Executable bool `json:"executable,omitempty"`
}

// 资源文件
type ResourceFile struct {
	// 资源路径
	Path string `json:"path"`
	// 文件内容
	Content string `json:"content,omitempty"`
	// 是否二进制
	Binary bool `json:"binary"`
	// 字节数
	Size int64 `json:"size"`
	// 编码内容
	ContentBase64 string `json:"contentBase64,omitempty"`
	// 可执行权限
	Executable bool `json:"executable,omitempty"`
}

// Resources 按稳定路径列出目录资源，不读取排除项或跟随符号链接。
func (a Archive) Resources(root string, excluded []string) ([]ResourceEntry, error) {
	resources := []ResourceEntry{}
	var total int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return a.IO
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return a.Invalid
		}
		for _, omitted := range excluded {
			if relative == omitted || strings.HasPrefix(relative, omitted+string(filepath.Separator)) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return a.Invalid
		}
		if _, err := a.validateImportRelativePath(relative); err != nil {
			return err
		}
		if len(resources) >= maxImportFileCount {
			return a.Limit
		}
		resource := ResourceEntry{Path: filepath.ToSlash(relative), Directory: entry.IsDir()}
		if !entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return a.IO
			}
			if !info.Mode().IsRegular() {
				return a.Invalid
			}
			if info.Size() > maxImportedFileBytes {
				return a.Limit
			}
			file, err := os.Open(path)
			if err != nil {
				return a.IO
			}
			hash := sha256.New()
			size, readErr := io.Copy(hash, io.LimitReader(file, maxImportedFileBytes+1))
			closeErr := file.Close()
			if readErr != nil || closeErr != nil {
				return a.IO
			}
			if size > maxImportedFileBytes || total+size > maxImportedBytes {
				return a.Limit
			}
			total += size
			resource.Size, resource.Digest, resource.Executable = size, hex.EncodeToString(hash.Sum(nil)), info.Mode().Perm()&0o111 != 0
		}
		resources = append(resources, resource)
		return nil
	})
	return resources, err
}

// ReadResource 只读取资源清单内的普通文件，正文按需返回且限制为 1 MiB。
func (a Archive) ReadResource(root, requested string, resources []ResourceEntry) (ResourceFile, error) {
	var response ResourceFile
	relative, err := a.validateImportRelativePath(requested)
	if err != nil {
		return response, err
	}
	listed := false
	for _, resource := range resources {
		if resource.Path == filepath.ToSlash(relative) && !resource.Directory {
			listed = true
			break
		}
	}
	if !listed {
		return response, a.Invalid
	}
	// 再逐级核对路径，防止目录在清单读取后被替换为符号链接。
	path := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		path = filepath.Join(path, component)
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return response, a.Invalid
		}
	}
	file, err := os.Open(path)
	if err != nil {
		return response, a.IO
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return response, a.Invalid
	}
	if info.Size() > maxResourcePreviewBytes {
		return response, a.Limit
	}
	content, err := io.ReadAll(io.LimitReader(file, maxResourcePreviewBytes+1))
	if err != nil {
		return response, a.IO
	}
	if len(content) > maxResourcePreviewBytes {
		return response, a.Limit
	}
	response = ResourceFile{Path: filepath.ToSlash(relative), Size: int64(len(content)), Binary: !utf8.Valid(content) || bytes.ContainsRune(content, 0), Executable: info.Mode().Perm()&0o111 != 0}
	if response.Binary {
		response.ContentBase64 = base64.StdEncoding.EncodeToString(content)
	} else {
		response.Content = string(content)
	}
	return response, nil
}
