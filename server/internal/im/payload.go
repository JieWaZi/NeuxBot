package im

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// SplitText 按 Unicode 字符和换行边界分割平台正文。
func SplitText(text string, limit int) []string {
	if text == "" {
		return nil
	}
	runes := []rune(text)
	parts := []string{}
	for len(runes) > limit {
		cut := limit
		for index := limit; index > limit/2; index-- {
			if runes[index-1] == '\n' {
				cut = index
				break
			}
		}
		parts = append(parts, string(runes[:cut]))
		runes = runes[cut:]
	}
	if len(runes) > 0 {
		parts = append(parts, string(runes))
	}
	return parts
}

// SafeFilename 清理不可信附件名称，保持受管路径内的单一文件名。
func SafeFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune("<>:\"|?*", r) {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if name == "" {
		name = "attachment"
	}
	runes := []rune(name)
	if len(runes) > 100 {
		name = string(runes[:100])
	}
	return name
}

// FreezeFile 只读取执行授权根目录内的文件，并原子冻结到投递暂存目录。
func FreezeFile(dataDirectory, id string, index int, path string, roots []string) (models.IMFile, error) {
	var result models.IMFile
	if !filepath.IsAbs(path) && len(roots) > 0 {
		path = filepath.Join(roots[0], path)
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return result, status_error.IMFileInvalid
	}
	realPath, err = filepath.Abs(realPath)
	if err != nil {
		return result, status_error.IMFileInvalid
	}
	var source *os.File
	for _, root := range roots {
		actual, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		actual, err = filepath.Abs(actual)
		if err != nil || actual != filepath.Clean(root) {
			continue
		}
		relative, err := filepath.Rel(actual, realPath)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			continue
		}
		directory, err := os.OpenRoot(actual)
		if err != nil {
			continue
		}
		source, err = directory.Open(relative)
		directory.Close()
		if err == nil {
			break
		}
	}
	if source == nil {
		return result, status_error.IMFileInvalid
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > constants.IMFileMaximumBytes {
		return result, status_error.IMFileInvalid
	}
	folder := filepath.Join(dataDirectory, "im", "deliveries", id)
	if err := os.MkdirAll(folder, 0700); err != nil {
		return result, status_error.IMFileInvalid
	}
	result = models.IMFile{Name: SafeFilename(filepath.Base(path)), Kind: "file"}
	result.Path = filepath.Join(folder, strconv.Itoa(index)+"-"+result.Name)
	temporary, err := os.CreateTemp(folder, "delivery-staging-")
	if err != nil {
		return result, status_error.IMFileInvalid
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(source, constants.IMFileMaximumBytes+1))
	result.Digest = hex.EncodeToString(hash.Sum(nil))
	syncErr := temporary.Sync()
	closeErr := temporary.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil || size > constants.IMFileMaximumBytes {
		return result, status_error.IMFileInvalid
	}
	if err := os.Rename(temporaryName, result.Path); err != nil {
		return result, status_error.IMFileInvalid
	}
	extension := strings.ToLower(filepath.Ext(result.Name))
	if extension == ".png" || extension == ".jpg" || extension == ".jpeg" || extension == ".gif" || extension == ".webp" {
		result.Kind = "image"
	}
	return result, nil
}
