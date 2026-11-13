// pluginpack 使用宿主统一生成器构建独立 CLI 插件工件，不启动 Core。
package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/packagefiles"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
)

// artifact 描述发布根内可独立解包的无秘密目标。
type artifact struct {
	// Runtime 是 CLI 家族。
	Runtime string `json:"runtime"`
	// Profile 是目标格式配置档。
	Profile string `json:"profile"`
	// Format 是真实加载格式。
	Format string `json:"format"`
	// ContentDigest 是解包后的内容摘要。
	ContentDigest string `json:"contentDigest"`
	// ArchiveSHA256 是 ZIP 文件摘要。
	ArchiveSHA256 string `json:"archiveSHA256"`
	// Directory 是发布根内目标目录。
	Directory string `json:"directory"`
	// Archive 是发布根内 ZIP 路径。
	Archive string `json:"archive"`
	// Files 是目标内的相对文件清单。
	Files []string `json:"files"`
}

// releaseManifest 绑定唯一来源、生成规则和发布工件。
type releaseManifest struct {
	// SchemaVersion 是当前开发期工件契约版本。
	SchemaVersion int `json:"schemaVersion"`
	// SourceID 是来源公开身份。
	SourceID string `json:"sourceID"`
	// SourceDigest 是原始输入树的摘要，对应 source-digest 参数。
	SourceDigest string `json:"sourceDigest"`
	// NormalizedSourceDigest 是统一生成器实际消费的冻结内容摘要。
	NormalizedSourceDigest string `json:"normalizedSourceDigest"`
	// Name 是包技术名。
	Name string `json:"name"`
	// Version 是包业务版本。
	Version string `json:"version"`
	// Artifacts 是确定顺序的发布目标。
	Artifacts []artifact `json:"artifacts"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("pluginpack", flag.ContinueOnError)
	source := flags.String("source", "", "portable source root")
	output := flags.String("output", "", "empty output directory")
	profile := flags.String("profile", "all", "CLI family or all")
	sourceID := flags.String("source-id", "", "public source identity")
	expected := flags.String("source-digest", "", "verified source content digest")
	if err := flags.Parse(args); err != nil {
		return status_error.PluginContentInvalid
	}
	if *source == "" || *output == "" || flags.NArg() != 0 {
		return status_error.PluginContentInvalid
	}
	root, err := filepath.Abs(*source)
	if err != nil {
		return status_error.PluginContentInvalid
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return status_error.PluginContentInvalid
	}
	destination, err := filepath.Abs(*output)
	if err != nil {
		return status_error.PluginContentInvalid
	}
	if err := plugins.ValidatePreparationBoundary(root, destination); err != nil {
		return err
	}
	if err := plugins.ValidatePreparationBoundary(root, filepath.Dir(destination)); err != nil {
		return err
	}
	if !validPublicSourceID(*sourceID) {
		return status_error.PluginContentInvalid
	}
	if entries, err := os.ReadDir(destination); err == nil && len(entries) > 0 {
		return status_error.PluginVersionConflict
	} else if err != nil && !os.IsNotExist(err) {
		return status_error.PluginStorageError
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return status_error.PluginStorageError
	}
	stage, err := os.MkdirTemp(filepath.Dir(destination), ".pluginpack-")
	if err != nil {
		return status_error.PluginStorageError
	}
	defer os.RemoveAll(stage)
	frozen, err := freezeReleaseSource(root, filepath.Join(stage, ".source"), *expected, plugins.CopyDeliveryTreeWithDigest)
	if err != nil {
		return err
	}
	root, digest, pkg := frozen.Root, frozen.NormalizedDigest, frozen.Package
	if *sourceID == "" {
		*sourceID = pkg.Manifest.Name
	}
	cache := filepath.Join(stage, ".targets")
	targets := []plugins.NativeTarget{}
	if *profile == "all" {
		targets, err = plugins.PrepareTargets(context.Background(), cache, *sourceID, pkg, root)
	} else {
		var target plugins.NativeTarget
		target, err = plugins.PrepareNativeTargetWithDigest(context.Background(), cache, *sourceID, pkg, root, *profile, digest)
		targets = append(targets, target)
	}
	if err != nil {
		return err
	}
	manifest := releaseManifest{SchemaVersion: 1, SourceID: *sourceID, SourceDigest: frozen.SourceDigest, NormalizedSourceDigest: frozen.NormalizedDigest, Name: pkg.Manifest.Name, Version: pkg.Manifest.Version, Artifacts: []artifact{}}
	for _, target := range targets {
		name := pkg.Manifest.Name + "-" + target.Runtime
		directory := filepath.Join(stage, name)
		if err := os.Mkdir(directory, 0700); err != nil {
			return status_error.PluginStorageError
		}
		if err := plugins.CopyDeliveryTree(target.Root, directory); err != nil {
			return err
		}
		contentDigest, err := plugins.ContentDigest(directory)
		if err != nil {
			return err
		}
		if contentDigest != target.Digest {
			return status_error.PluginVersionConflict
		}
		files, sha, err := writeZIP(directory, filepath.Join(stage, name+".zip"))
		if err != nil {
			return err
		}
		manifest.Artifacts = append(manifest.Artifacts, artifact{Runtime: target.Runtime, Profile: target.Profile, Format: target.Format, ContentDigest: contentDigest, ArchiveSHA256: sha, Directory: name, Archive: name + ".zip", Files: files})
	}
	if len(manifest.Artifacts) == 0 {
		return status_error.PluginCapabilityUnsupported
	}
	if err := os.RemoveAll(filepath.Join(stage, ".source")); err != nil {
		return status_error.PluginStorageError
	}
	if err := os.RemoveAll(cache); err != nil {
		return status_error.PluginStorageError
	}
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return status_error.PluginContentInvalid
	}
	if err := os.WriteFile(filepath.Join(stage, "artifacts.json"), append(body, '\n'), 0600); err != nil {
		return status_error.PluginStorageError
	}
	if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
		return status_error.PluginStorageError
	}
	if err := os.Rename(stage, destination); err != nil {
		return status_error.PluginStorageError
	}
	return nil
}

func writeZIP(root, path string) ([]string, string, error) {
	file, err := os.Create(path)
	if err != nil {
		return nil, "", status_error.PluginStorageError
	}
	digest := sha256.New()
	writer := zip.NewWriter(io.MultiWriter(file, digest))
	files := []string{}
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return status_error.PluginStorageError
		}
		if path == root {
			return nil
		}
		if !entry.IsDir() && !entry.Type().IsRegular() && entry.Type()&os.ModeSymlink == 0 {
			return status_error.PluginContentInvalid
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return status_error.PluginContentInvalid
		}
		info, err := entry.Info()
		if err != nil {
			return status_error.PluginStorageError
		}
		name := filepath.ToSlash(relative)
		if entry.IsDir() {
			name += "/"
		}
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(info.Mode())
		header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		output, err := writer.CreateHeader(header)
		if err != nil {
			return status_error.PluginStorageError
		}
		files = append(files, header.Name)
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return status_error.PluginStorageError
			}
			if _, err := io.WriteString(output, link); err != nil {
				return status_error.PluginStorageError
			}
			return nil
		}
		input, err := os.Open(path)
		if err != nil {
			return status_error.PluginStorageError
		}
		_, copyErr := io.Copy(output, input)
		closeErr := input.Close()
		if copyErr != nil || closeErr != nil {
			return status_error.PluginStorageError
		}
		return nil
	})
	zipErr := writer.Close()
	closeErr := file.Close()
	if walkErr != nil {
		return nil, "", walkErr
	}
	if zipErr != nil || closeErr != nil {
		return nil, "", status_error.PluginStorageError
	}
	unpacked, err := os.MkdirTemp(filepath.Dir(path), ".zip-verify-")
	if err != nil {
		return nil, "", status_error.PluginStorageError
	}
	defer os.RemoveAll(unpacked)
	archive := packagefiles.Archive{Invalid: status_error.PluginContentInvalid, IO: status_error.PluginStorageError, Limit: status_error.PluginContentLimitExceeded, PreserveExecutable: true}
	if err := archive.ExtractZIP(path, unpacked); err != nil {
		return nil, "", err
	}
	expected, err := plugins.ContentDigest(root)
	if err != nil {
		return nil, "", err
	}
	actual, err := plugins.ContentDigest(unpacked)
	if err != nil {
		return nil, "", err
	}
	if expected != actual {
		return nil, "", status_error.PluginVersionConflict
	}
	return files, hex.EncodeToString(digest.Sum(nil)), nil
}
