package main

import (
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
)

// releaseSource 保留原始来源与规范化冻结内容的独立摘要。
type releaseSource struct {
	// Root 是后续唯一消费的规范化冻结根。
	Root string
	// SourceDigest 是调用方原始输入树的摘要。
	SourceDigest string
	// NormalizedDigest 是冻结根实际链接与内容的摘要。
	NormalizedDigest string
	// Package 是仅从规范化冻结根解析的组件清单。
	Package plugins.Package
}

// freezeReleaseSource 先取得独立快照，再绑定原包摘要与后续全部生成输入。
func freezeReleaseSource(source, destination, expected string) (releaseSource, error) {
	empty := releaseSource{}
	if err := validatePublishSource(source, false); err != nil {
		return empty, err
	}
	before, err := plugins.ContentDigest(source)
	if err != nil {
		return empty, err
	}
	if expected != "" && expected != before {
		return empty, status_error.PluginVersionConflict
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		return empty, status_error.PluginStorageError
	}
	if err := plugins.CopyDeliveryTreeWithDigest(source, destination, before); err != nil {
		return empty, err
	}
	frozen, err := plugins.ContentDigest(destination)
	if err != nil {
		return empty, err
	}
	after, err := plugins.ContentDigest(source)
	if err != nil {
		return empty, err
	}
	if before != after {
		return empty, status_error.PluginVersionConflict
	}
	if err := validatePublishSource(destination, true); err != nil {
		return empty, err
	}
	pkg, err := plugins.Load(destination)
	if err != nil {
		return empty, err
	}
	return releaseSource{Root: destination, SourceDigest: before, NormalizedDigest: frozen, Package: pkg}, nil
}

func validatePublishSource(root string, published bool) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return status_error.PluginStorageError
		}
		name := strings.ToLower(entry.Name())
		if name == ".git" || name == ".ssh" || name == ".aws" || name == ".gnupg" || name == ".npmrc" || name == ".netrc" || name == ".pypirc" || name == ".ds_store" {
			return status_error.PluginContentInvalid
		}
		if name == ".env" || strings.HasPrefix(name, ".env.") {
			if !(strings.HasSuffix(name, ".example") || strings.HasSuffix(name, ".sample") || strings.HasSuffix(name, ".template")) {
				return status_error.PluginContentInvalid
			}
		}
		if entry.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return status_error.PluginStorageError
			}
			// 原始输入的合法内部链接可规范化，发布树才强制不含机器绝对路径。
			if published && filepath.IsAbs(link) {
				return status_error.PluginContentInvalid
			}
		}
		return nil
	})
}

func validPublicSourceID(value string) bool {
	if value == "" {
		return true
	}
	if address, err := url.Parse(value); err == nil && address.Scheme == "https" {
		return address.Host != "" && address.User == nil && address.RawQuery == "" && address.Fragment == "" && !strings.Contains(address.Path, "..")
	}
	if index := strings.IndexByte(value, ':'); index >= 0 {
		commit := value[:index]
		if len(commit) != 40 && len(commit) != 64 {
			return false
		}
		if _, err := hex.DecodeString(commit); err != nil {
			return false
		}
		value = value[index+1:]
	}
	if value == "" || filepath.IsAbs(value) || strings.ContainsAny(value, "\\:@?#") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, character := range part {
			if !(unicode.IsLetter(character) || unicode.IsDigit(character) || strings.ContainsRune("._-", character)) {
				return false
			}
		}
	}
	return true
}
