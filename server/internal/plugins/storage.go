package plugins

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/packagefiles"
)

var pluginArchive = packagefiles.Archive{Invalid: status_error.PluginContentInvalid, Limit: status_error.PluginContentLimitExceeded, IO: status_error.PluginStorageError, PreserveExecutable: true}

// StagedPackage 保存本次准备的内容和解析结果。
type StagedPackage struct {
	// Directory 是调用方用完后需要删除的暂存目录。
	Directory string
	// Root 是已准备内容的绝对目录。
	Root string
	// Digest 是完整内容摘要。
	Digest string
	// Package 是确定性解析的派生描述。
	Package Package
}

// Stage 复制本地目录或有界解压 ZIP，提交只引用受管快照。
func Stage(parent, location string) (StagedPackage, error) { return StageSource(parent, location, ".") }

// StageSource 保存完整来源 ZIP，entry 只选择逻辑入口。
func StageSource(parent, location, entry string) (StagedPackage, error) {
	var result StagedPackage
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return result, storageFailure(err)
	}
	temporary, err := os.MkdirTemp(parent, "plugin-")
	if err != nil {
		return result, storageFailure(err)
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(temporary)
		}
	}()
	root := filepath.Join(temporary, "content")
	if err := os.Mkdir(root, 0o700); err != nil {
		return result, storageFailure(err)
	}
	info, err := os.Stat(location)
	if err != nil {
		return result, storageFailure(err)
	}
	if info.IsDir() {
		if err := validateLinks(location); err != nil {
			return result, err
		}
		err = pluginArchive.CopyDirectory(location, root)
	} else if info.Mode().IsRegular() && strings.EqualFold(filepath.Ext(location), ".zip") && info.Size() <= 100*1024*1024 {
		err = pluginArchive.ExtractZIP(location, root)
	} else {
		return result, status_error.PluginContentInvalid
	}
	if err != nil {
		return result, err
	}
	if err := validateLinks(root); err != nil {
		return result, err
	}
	pkg, err := LoadSource(root, entry)
	if err != nil {
		return result, err
	}
	digest, err := pluginArchive.Digest(root)
	if err != nil {
		return result, err
	}
	keep = true
	return StagedPackage{Directory: temporary, Root: root, Digest: digest, Package: pkg}, nil
}

// CopyContent 将完整内容原子准备为不可变目录，调用方持久化相同摘要。
func CopyContent(parent, source, digest string) (string, error) {
	if len(digest) != 64 || strings.ContainsAny(digest, "/\\") {
		return "", status_error.PluginContentInvalid
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", storageFailure(err)
	}
	target := filepath.Join(parent, digest)
	if _, err := os.Stat(target); err == nil {
		actual, err := pluginArchive.Digest(target)
		if err != nil {
			return "", err
		}
		if actual != digest {
			return "", status_error.PluginVersionConflict
		}
		return target, nil
	}
	stage, err := os.MkdirTemp(parent, "content-")
	if err != nil {
		return "", storageFailure(err)
	}
	defer os.RemoveAll(stage)
	if err := pluginArchive.CopyDirectory(source, stage); err != nil {
		return "", err
	}
	if err := validateLinks(stage); err != nil {
		return "", err
	}
	actual, err := pluginArchive.Digest(stage)
	if err != nil {
		return "", err
	}
	if actual != digest {
		return "", status_error.PluginVersionConflict
	}
	if err := os.Rename(stage, target); err != nil {
		return "", storageFailure(err)
	}
	return target, nil
}
func validateLinks(root string) error {
	graph, err := captureDeliveryGraph(root)
	if err != nil {
		return err
	}
	if _, err = graph.linkTargets(); err != nil {
		return err
	}
	return graph.requireActualReachability()
}

func storageFailure(err error) error {
	logx.Errorf("plugin storage operation failed: %v", err)
	return status_error.PluginStorageError
}

// ContentPath 只解析受管不可变内容路径，拒绝目录替换和链接越界。
func ContentPath(dataDirectory, relative string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(relative))
	prefix := filepath.Join("plugins", "content") + string(filepath.Separator)
	if filepath.IsAbs(clean) || !strings.HasPrefix(clean, prefix) {
		return "", status_error.PluginContentInvalid
	}
	root, err := filepath.EvalSymlinks(filepath.Join(dataDirectory, "plugins", "content"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", status_error.PluginNotReady
		}
		return "", storageFailure(err)
	}
	actual, err := filepath.EvalSymlinks(filepath.Join(dataDirectory, clean))
	if err != nil {
		if os.IsNotExist(err) {
			return "", status_error.PluginNotReady
		}
		return "", storageFailure(err)
	}
	rel, err := filepath.Rel(root, actual)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", status_error.PluginContentInvalid
	}
	return actual, nil
}

// VerifyContent 在安装和执行接纳时复验不可变内容，避免交付已经变化的文件。
func VerifyContent(root, digest string) error {
	actual, err := ContentDigest(root)
	if err != nil {
		return err
	}
	if actual != digest {
		return status_error.PluginVersionConflict
	}
	return nil
}

// ContentDigest 返回受管目标完整摘要，供投射与原生重试冻结同一实际内容。
func ContentDigest(root string) (string, error) {
	if err := validateLinks(root); err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", storageFailure(err)
	}
	return pluginArchive.Digest(canonical)
}
