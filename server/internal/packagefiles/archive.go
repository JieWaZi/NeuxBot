// Package packagefiles 复用技能与插件的有界归档、路径校验和内容摘要机制。
package packagefiles

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	maxImportedBytes     int64 = 500 * 1024 * 1024
	maxImportedFileBytes int64 = 50 * 1024 * 1024
	maxImportFileCount         = 5000
	maxImportPathBytes         = 512
)

// Archive 为调用领域选择准确的稳定失败语义，共用真实文件处理机制。
type Archive struct {
	// Invalid 表示不合法的路径、文件或内容。
	Invalid error
	// Limit 表示内容超过有界限制。
	Limit error
	// IO 表示来源读取、复制或解压失败。
	IO error
	// PreserveExecutable 表示保留所有普通文件执行位，否则仅保留 scripts 目录执行位。
	PreserveExecutable bool
}

// ExtractZIP 有界解压并拒绝越界路径、重复路径和特殊文件。
func (a Archive) ExtractZIP(path string, destination string) error {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return a.Invalid
	}
	defer archive.Close()
	fileCount := 0
	var importedBytes int64
	foldedPaths := make(map[string]string, len(archive.File))
	for _, file := range archive.File {
		name, err := a.validateImportRelativePath(file.Name)
		if err != nil {
			return err
		}
		folded := strings.ToLower(filepath.ToSlash(name))
		if _, exists := foldedPaths[folded]; exists {
			return a.Invalid
		}
		foldedPaths[folded] = name
		target := filepath.Join(destination, name)
		if err := a.validateZipParentPath(destination, filepath.Dir(target), name); err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return a.IO
			}
			continue
		}
		isSymlink := file.Mode()&os.ModeSymlink != 0
		if !file.Mode().IsRegular() && !isSymlink {
			return a.Invalid
		}
		fileCount++
		if fileCount > maxImportFileCount {
			return a.Limit
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return a.IO
		}
		input, err := file.Open()
		if err != nil {
			return a.IO
		}
		if isSymlink {
			linkTarget, readErr := io.ReadAll(io.LimitReader(input, maxImportedFileBytes+1))
			closeErr := input.Close()
			if readErr != nil {
				return a.IO
			}
			if closeErr != nil {
				return a.IO
			}
			if int64(len(linkTarget)) > maxImportedFileBytes {
				return a.Limit
			}
			if err := os.Symlink(string(linkTarget), target); err != nil {
				return a.IO
			}
			importedBytes += int64(len(linkTarget))
			if importedBytes > maxImportedBytes {
				return a.Limit
			}
			continue
		}
		mode := os.FileMode(0o600)
		if file.Mode().Perm()&0o111 != 0 && (a.PreserveExecutable || a.pathContainsDirectory(name, "scripts")) {
			mode = 0o700
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
		var written int64
		if err == nil {
			written, err = io.Copy(output, io.LimitReader(input, maxImportedFileBytes+1))
			_ = output.Close()
		}
		_ = input.Close()
		if err != nil {
			return a.IO
		}
		if written > maxImportedFileBytes {
			return a.Limit
		}
		importedBytes += written
		if importedBytes > maxImportedBytes {
			return a.Limit
		}
	}
	return nil
}

func (a Archive) pathContainsDirectory(relative string, directory string) bool {
	parts := strings.Split(filepath.ToSlash(relative), "/")
	for index := 0; index+1 < len(parts); index++ {
		if parts[index] == directory {
			return true
		}
	}
	return false
}

func (a Archive) validateImportRelativePath(value string) (string, error) {
	if !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') {
		return "", a.Invalid
	}
	cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
	if cleaned == "." || filepath.IsAbs(filepath.FromSlash(cleaned)) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", a.Invalid
	}
	if len([]byte(cleaned)) > maxImportPathBytes {
		return "", a.Limit
	}
	for _, component := range strings.Split(cleaned, "/") {
		if component == "" || strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
			return "", a.Invalid
		}
		for _, character := range component {
			if character < 32 || strings.ContainsRune(`<>:"\|?*`, character) {
				return "", a.Invalid
			}
		}
		base := strings.ToUpper(strings.SplitN(component, ".", 2)[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" ||
			(len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9') {
			return "", a.Invalid
		}
	}
	return filepath.Clean(filepath.FromSlash(cleaned)), nil
}

// Digest 校验完整目录并计算包含执行位与链接目标的摘要。
func (a Archive) Digest(root string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", a.Invalid
	}
	fileCount := 0
	var importedBytes int64
	foldedPaths := make(map[string]string)
	seenFiles := make([]os.FileInfo, 0)
	hash := sha256.New()
	// 路径有长度前缀，载荷固定为 SHA-256；文件内容不能模拟下一条目录记录。
	record := func(name string, kind byte, payload [sha256.Size]byte) {
		_, _ = hash.Write(binary.BigEndian.AppendUint64(nil, uint64(len(name))))
		_, _ = io.WriteString(hash, name)
		_, _ = hash.Write([]byte{kind})
		_, _ = hash.Write(payload[:])
	}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return a.IO
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return a.IO
		}
		if relative == "." {
			return nil
		}
		if relative == ".git" && entry.IsDir() {
			return filepath.SkipDir
		}
		normalized, err := a.validateImportRelativePath(filepath.ToSlash(relative))
		if err != nil {
			return err
		}
		normalized = filepath.ToSlash(normalized)
		folded := strings.ToLower(normalized)
		if _, exists := foldedPaths[folded]; exists {
			return a.Invalid
		}
		foldedPaths[folded] = normalized
		if entry.IsDir() {
			record(normalized, 3, sha256.Sum256(nil))
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return a.IO
			}
			fileCount++
			if fileCount > maxImportFileCount {
				return a.Limit
			}
			if int64(len(linkTarget)) > maxImportedFileBytes {
				return a.Limit
			}
			record(normalized, 2, sha256.Sum256([]byte(linkTarget)))
			importedBytes += int64(len(linkTarget))
			if importedBytes > maxImportedBytes {
				return a.Limit
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return a.IO
		}
		if !info.Mode().IsRegular() {
			return a.Invalid
		}
		for _, existing := range seenFiles {
			if os.SameFile(existing, info) {
				return a.Invalid
			}
		}
		seenFiles = append(seenFiles, info)
		fileCount++
		if fileCount > maxImportFileCount {
			return a.Limit
		}
		file, err := os.Open(path)
		if err != nil {
			return a.IO
		}
		modeMarker := byte(0)
		if info.Mode().Perm()&0o111 != 0 {
			modeMarker = 1
		}
		fileHash := sha256.New()
		written, copyErr := io.Copy(fileHash, io.LimitReader(file, maxImportedFileBytes+1))
		closeErr := file.Close()
		if copyErr != nil {
			return a.IO
		}
		if closeErr != nil {
			return a.IO
		}
		if written > maxImportedFileBytes {
			return a.Limit
		}
		if written != info.Size() {
			return a.Invalid
		}
		var contentHash [sha256.Size]byte
		copy(contentHash[:], fileHash.Sum(nil))
		record(normalized, modeMarker, contentHash)
		importedBytes += written
		if importedBytes > maxImportedBytes {
			return a.Limit
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// CopyDirectory 有界复制目录并保持指定的程序执行位。
func (a Archive) CopyDirectory(source, target string) error {
	seenFiles := make([]os.FileInfo, 0)
	foldedPaths := make(map[string]string)
	fileCount := 0
	var copiedBytes int64
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return a.IO
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return a.IO
		}
		if relative == "." {
			return nil
		}
		if relative == ".git" && entry.IsDir() {
			return filepath.SkipDir
		}
		normalized, pathErr := a.validateImportRelativePath(filepath.ToSlash(relative))
		if pathErr != nil {
			return pathErr
		}
		normalized = filepath.ToSlash(normalized)
		folded := strings.ToLower(normalized)
		if _, exists := foldedPaths[folded]; exists {
			return a.Invalid
		}
		foldedPaths[folded] = normalized
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			if err := os.MkdirAll(destination, 0o700); err != nil {
				return a.IO
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return a.IO
			}
			fileCount++
			if fileCount > maxImportFileCount {
				return a.Limit
			}
			if int64(len(linkTarget)) > maxImportedFileBytes {
				return a.Limit
			}
			if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
				return a.IO
			}
			if err := os.Symlink(linkTarget, destination); err != nil {
				return a.IO
			}
			copiedBytes += int64(len(linkTarget))
			if copiedBytes > maxImportedBytes {
				return a.Limit
			}
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return a.IO
		}
		if !info.Mode().IsRegular() {
			return a.Invalid
		}
		for _, existing := range seenFiles {
			if os.SameFile(existing, info) {
				return a.Invalid
			}
		}
		seenFiles = append(seenFiles, info)
		fileCount++
		if fileCount > maxImportFileCount {
			return a.Limit
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return a.IO
		}
		input, err := os.Open(path)
		if err != nil {
			return a.IO
		}
		mode := os.FileMode(0o600)
		if info.Mode().Perm()&0o111 != 0 && (a.PreserveExecutable || a.pathContainsDirectory(relative, "scripts")) {
			mode = 0o700
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
		if err != nil {
			_ = input.Close()
			return a.IO
		}
		written, copyErr := io.Copy(output, io.LimitReader(input, maxImportedFileBytes+1))
		inputCloseErr := input.Close()
		closeErr := output.Close()
		if copyErr != nil {
			return a.IO
		}
		if inputCloseErr != nil {
			return a.IO
		}
		if closeErr != nil {
			return a.IO
		}
		if written > maxImportedFileBytes {
			return a.Limit
		}
		copiedBytes += written
		if copiedBytes > maxImportedBytes {
			return a.Limit
		}
		return nil
	})
}

func (a Archive) validateZipParentPath(root string, parent string, entryName string) error {
	relative, err := filepath.Rel(root, parent)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return a.Invalid
	}
	if relative == "." {
		return nil
	}
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			return nil
		}
		if statErr != nil {
			return a.IO
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return a.Invalid
		}
	}
	return nil
}
