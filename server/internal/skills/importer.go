// Package skills 提供技能导入、扫描和应用数据目录归档能力。
package skills

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/packagefiles"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

var plainCloneContext = git.PlainCloneContext

const (
	maxZipSourceBytes       int64 = 100 * 1024 * 1024
	maxImportedBytes        int64 = 500 * 1024 * 1024
	maxImportedFileBytes    int64 = 50 * 1024 * 1024
	maxImportFileCount            = 5000
	maxImportPathBytes            = 512
	stagedBatchManifestName       = "batch.json"
	stagedBatchContentName        = "content"
)

// Source 描述一次 Git 或 ZIP 技能导入。
type Source struct {
	// Kind 是导入类型，取值为 git 或 zip。
	Kind constants.SkillSource
	// Location 是 Git 地址或本地 ZIP 文件路径。
	Location string
	// Ref 是 Git 分支、标签或提交引用。
	Ref string
	// Subpath 是 Git 仓库内的扫描子目录。
	Subpath string
}

// Package 是扫描到的一个技能包目录。
type Package struct {
	// Name 是 SKILL.md Frontmatter 声明的 Runtime 技能名。
	Name string `json:"name"`
	// Description 是 SKILL.md Frontmatter 声明的技能说明。
	Description string `json:"description"`
	// Directory 是 staging 中技能包的绝对路径。
	Directory string `json:"-"`
	// RelativePath 是批次内容根目录内的候选相对路径。
	RelativePath string `json:"relativePath"`
}

// StagedBatch 是完成来源获取和结构校验、等待用户选择的导入批次。
type StagedBatch struct {
	// Token 是后续安装时引用该服务端批次的不可猜测令牌。
	Token string
	// Root 是批次受管临时目录，仅供服务端清理。
	Root string
	// Source 是完成规范化且可以记录到技能包的来源。
	Source Source
	// Packages 是本批次扫描到的候选技能包。
	Packages []Package
}

// stagedBatchManifest 保存跨请求重新打开导入批次所需的服务端事实。
type stagedBatchManifest struct {
	// Source 是完成规范化的 Git 或 ZIP 来源。
	Source Source `json:"source"`
	// Packages 是只保留相对路径和元数据的候选列表。
	Packages []Package `json:"packages"`
	// ContentDigest 是扫描完成时整棵 staging 内容树的不可逆摘要。
	ContentDigest string `json:"contentDigest"`
}

// Import 将来源拉取或解压到受管临时目录，并扫描根、一级和二级目录中的 SKILL.md。
func Import(ctx context.Context, temporaryParent string, source Source) ([]Package, string, error) {
	batch, err := StageImport(ctx, temporaryParent, source)
	if err != nil {
		return nil, "", err
	}
	return batch.Packages, batch.Root, nil
}

// StageImport 获取并校验来源，持久化一个等待用户选择的服务端导入批次。
func StageImport(ctx context.Context, temporaryParent string, source Source) (StagedBatch, error) {
	temporaryParent = strings.TrimSpace(temporaryParent)
	if temporaryParent == "" {
		return StagedBatch{}, status_error.SkillStorageError
	}
	if err := os.MkdirAll(temporaryParent, 0o700); err != nil {
		return StagedBatch{}, status_error.SkillStorageError
	}
	temporaryRoot, err := os.MkdirTemp(temporaryParent, "import-")
	if err != nil {
		return StagedBatch{}, status_error.SkillStorageError
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(temporaryRoot)
		}
	}()
	contentRoot := filepath.Join(temporaryRoot, stagedBatchContentName)
	root := contentRoot
	switch source.Kind {
	case constants.SkillSourceGit:
		normalized, normalizeErr := normalizeGitSource(source)
		if normalizeErr != nil {
			return StagedBatch{}, normalizeErr
		}
		if err := cloneGitSource(ctx, normalized, contentRoot); err != nil {
			return StagedBatch{}, err
		}
		source = normalized
	case constants.SkillSourceZip:
		info, statErr := os.Stat(source.Location)
		if statErr != nil {
			return StagedBatch{}, status_error.SkillImportFailed
		}
		if info.Size() > maxZipSourceBytes {
			return StagedBatch{}, status_error.SkillContentLimitExceeded
		}
		if err := os.MkdirAll(contentRoot, 0o700); err != nil {
			return StagedBatch{}, status_error.SkillStorageError
		}
		if err := extractZip(source.Location, contentRoot); err != nil {
			return StagedBatch{}, err
		}
	default:
		return StagedBatch{}, status_error.SkillImportSourceInvalid
	}
	contentDigest, err := validateStagedTree(contentRoot)
	if err != nil {
		return StagedBatch{}, err
	}
	if strings.TrimSpace(source.Subpath) != "" {
		subpath := filepath.Clean(source.Subpath)
		if filepath.IsAbs(subpath) || subpath == ".." || strings.HasPrefix(subpath, ".."+string(filepath.Separator)) {
			return StagedBatch{}, status_error.SkillImportSourceInvalid
		}
		root = filepath.Join(contentRoot, subpath)
	}
	packages, err := scan(root)
	if err != nil {
		return StagedBatch{}, err
	}
	for index := range packages {
		relative, relativeErr := filepath.Rel(contentRoot, packages[index].Directory)
		if relativeErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return StagedBatch{}, status_error.SkillContentInvalid
		}
		packages[index].RelativePath = filepath.ToSlash(relative)
	}
	manifestPackages := append([]Package(nil), packages...)
	manifest := stagedBatchManifest{
		Source: source, Packages: manifestPackages, ContentDigest: contentDigest,
	}
	for index := range manifest.Packages {
		manifest.Packages[index].Directory = ""
	}
	content, err := json.Marshal(manifest)
	if err != nil {
		return StagedBatch{}, status_error.SkillStorageError
	}
	if err := os.WriteFile(filepath.Join(temporaryRoot, stagedBatchManifestName), append(content, '\n'), 0o600); err != nil {
		return StagedBatch{}, status_error.SkillStorageError
	}
	cleanup = false
	return StagedBatch{
		Token: filepath.Base(temporaryRoot), Root: temporaryRoot,
		Source: source, Packages: packages,
	}, nil
}

// OpenStagedImport 重新打开服务端保存的导入批次并恢复候选绝对路径。
func OpenStagedImport(temporaryParent string, token string) (StagedBatch, error) {
	temporaryParent = strings.TrimSpace(temporaryParent)
	token = strings.TrimSpace(token)
	if temporaryParent == "" || token == "" || filepath.Base(token) != token || !strings.HasPrefix(token, "import-") {
		return StagedBatch{}, status_error.SkillImportIntegrityInvalid
	}
	root := filepath.Join(temporaryParent, token)
	manifestPath := filepath.Join(root, stagedBatchManifestName)
	info, err := os.Lstat(manifestPath)
	if err != nil || !info.Mode().IsRegular() {
		return StagedBatch{}, status_error.SkillImportIntegrityInvalid
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		return StagedBatch{}, status_error.SkillStorageError
	}
	var manifest stagedBatchManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return StagedBatch{}, status_error.SkillImportIntegrityInvalid
	}
	contentRoot := filepath.Join(root, stagedBatchContentName)
	contentDigest, err := validateStagedTree(contentRoot)
	if err != nil || manifest.ContentDigest == "" || contentDigest != manifest.ContentDigest {
		return StagedBatch{}, status_error.SkillImportIntegrityInvalid
	}
	for index := range manifest.Packages {
		relative := filepath.Clean(filepath.FromSlash(manifest.Packages[index].RelativePath))
		if filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return StagedBatch{}, status_error.SkillImportIntegrityInvalid
		}
		directory := filepath.Join(contentRoot, relative)
		metadata, metadataErr := ReadMetadata(directory)
		if metadataErr != nil || metadata.Name != manifest.Packages[index].Name || metadata.Description != manifest.Packages[index].Description {
			return StagedBatch{}, status_error.SkillImportIntegrityInvalid
		}
		manifest.Packages[index].Directory = directory
	}
	return StagedBatch{
		Token: token, Root: root, Source: manifest.Source,
		Packages: manifest.Packages,
	}, nil
}

func normalizeGitSource(source Source) (Source, error) {
	location := strings.TrimSpace(source.Location)
	parsed, err := url.Parse(location)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return Source{}, status_error.SkillImportSourceInvalid
	}
	if parsed.User != nil {
		return Source{}, status_error.SkillImportSourceInvalid
	}
	if parsed.RawQuery != "" {
		return Source{}, status_error.SkillImportSourceInvalid
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return Source{}, status_error.SkillImportSourceInvalid
	}

	normalized := source
	normalized.Location = location
	normalized.Ref = strings.TrimSpace(source.Ref)
	normalized.Subpath = strings.TrimSpace(source.Subpath)
	if !strings.EqualFold(parsed.Hostname(), "github.com") {
		return normalized, nil
	}

	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) < 2 {
		return Source{}, status_error.SkillImportSourceInvalid
	}
	if len(segments) >= 4 && segments[2] == "tree" {
		parsed.Path = "/" + segments[0] + "/" + strings.TrimSuffix(segments[1], ".git") + ".git"
		parsed.RawPath = ""
		parsed.RawQuery = ""
		parsed.Fragment = ""
		normalized.Location = parsed.String()
		if normalized.Ref == "" {
			normalized.Ref = segments[3]
		}
		urlSubpath := strings.Join(segments[4:], "/")
		if urlSubpath != "" {
			normalized.Subpath = filepath.ToSlash(filepath.Join(urlSubpath, normalized.Subpath))
		}
	}
	return normalized, nil
}

func cloneGitSource(ctx context.Context, source Source, destination string) error {
	ref := strings.TrimSpace(source.Ref)
	options := &git.CloneOptions{
		URL:          source.Location,
		Depth:        1,
		SingleBranch: true,
		Tags:         git.NoTags,
	}
	if parsed, err := url.Parse(source.Location); err == nil && parsed.Scheme == "" {
		options.Depth = 0
	}
	if ref != "" && !plumbing.IsHash(ref) {
		if strings.HasPrefix(ref, "refs/") {
			options.ReferenceName = plumbing.ReferenceName(ref)
		} else {
			options.ReferenceName = plumbing.NewBranchReferenceName(ref)
		}
	}
	repository, err := plainCloneContext(ctx, destination, false, options)
	if err != nil && ref != "" && !plumbing.IsHash(ref) && !strings.HasPrefix(ref, "refs/") {
		if cleanupErr := os.RemoveAll(destination); cleanupErr != nil {
			return status_error.SkillImportFailed
		}
		options.ReferenceName = plumbing.NewTagReferenceName(ref)
		repository, err = plainCloneContext(ctx, destination, false, options)
	}
	if err != nil {
		return status_error.SkillImportFailed
	}
	if !plumbing.IsHash(ref) {
		return nil
	}
	worktree, err := repository.Worktree()
	if err != nil {
		return status_error.SkillImportFailed
	}
	if err := worktree.Checkout(&git.CheckoutOptions{Hash: plumbing.NewHash(ref)}); err != nil {
		return status_error.SkillImportFailed
	}
	return nil
}

func extractZip(path, destination string) error { return skillArchive.ExtractZIP(path, destination) }

func validateStagedTree(root string) (string, error) { return skillArchive.Digest(root) }

func scan(root string) ([]Package, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, status_error.SkillContentInvalid
	}
	var result []Package
	var visit func(string, int) error
	visit = func(directory string, depth int) error {
		if depth > 2 {
			return nil
		}
		if info, err := os.Stat(filepath.Join(directory, "SKILL.md")); err == nil && !info.IsDir() {
			metadata, metadataErr := ReadMetadata(directory)
			if metadataErr != nil {
				return metadataErr
			}
			result = append(result, Package{
				Name:        metadata.Name,
				Description: metadata.Description,
				Directory:   directory,
			})
		}
		if depth == 2 {
			return nil
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			return status_error.SkillImportFailed
		}
		for _, entry := range entries {
			if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
				if err := visit(filepath.Join(directory, entry.Name()), depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(root, 0); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, status_error.SkillContentInvalid
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// Digest 校验完整技能目录，并返回包含脚本执行位的 SHA-256 摘要。
func Digest(directory string) (string, error) {
	return validateStagedTree(directory)
}

// CopyVersion 通过同文件系统 staging 原子保存一个不可变技能版本目录。
func CopyVersion(dataDirectory string, packageID string, version string, sourceDirectory string) (string, string, error) {
	dataDirectory = strings.TrimSpace(dataDirectory)
	packageID = strings.TrimSpace(packageID)
	version = strings.TrimSpace(version)
	if dataDirectory == "" || packageID == "" || filepath.Base(packageID) != packageID || version == "" || filepath.Base(version) != version {
		return "", "", status_error.SkillContentInvalid
	}
	relative := filepath.Join("skills", "versions", packageID, version)
	target := filepath.Join(dataDirectory, relative)
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", "", status_error.SkillStorageError
	}
	if _, err := os.Stat(target); err == nil {
		return "", "", status_error.Conflict
	} else if !os.IsNotExist(err) {
		return "", "", status_error.SkillStorageError
	}
	staging, err := os.MkdirTemp(parent, "version-staging-"+version+"-")
	if err != nil {
		return "", "", status_error.SkillStorageError
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := CopyDirectory(sourceDirectory, staging); err != nil {
		return "", "", err
	}
	digest, err := Digest(staging)
	if err != nil {
		return "", "", err
	}
	if err := os.Rename(staging, target); err != nil {
		return "", "", status_error.SkillStorageError
	}
	cleanup = false
	return relative, digest, nil
}

// CopyDirectory 安全复制完整技能包目录并保留软链接、拒绝其他特殊文件。
// CopyDirectory 安全复制完整技能目录并保持原有执行位规则。
func CopyDirectory(source, target string) error { return skillArchive.CopyDirectory(source, target) }

var skillArchive = packagefiles.Archive{Invalid: status_error.SkillContentInvalid, Limit: status_error.SkillContentLimitExceeded, IO: status_error.SkillImportFailed}
