// Package projectsource 解析并校验用户授权的本地项目目录。
package projectsource

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// Resolve 将用户选择的目录解析为稳定的真实路径索引。
func Resolve(value string) (models.ProjectSource, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(value))
	if err != nil {
		return models.ProjectSource{}, err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return models.ProjectSource{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return models.ProjectSource{}, status_error.ProjectSourceUnavailable
	}
	hash := sha256.Sum256([]byte(resolved))
	return models.ProjectSource{
		ID:   hex.EncodeToString(hash[:16]),
		Name: filepath.Base(resolved),
		Path: resolved,
	}, nil
}

// Available 判断保存的项目来源是否仍解析到同一真实目录。
func Available(source models.ProjectSource) bool {
	resolved, err := Resolve(source.Path)
	return err == nil && resolved.ID == source.ID
}
