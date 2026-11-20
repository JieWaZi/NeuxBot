package storage

import (
	"context"
	"crypto/sha256"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	managedskills "github.com/JieWaZi/NeuxBot/server/internal/skills"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
)

func (s *scanner) isSystemSkillFile(owner *scanRoot, root *os.Root, relative, name string, info fs.FileInfo) bool {
	path := filepath.ToSlash(relative)
	if !strings.Contains(path, "/"+managedskills.MaterializationDiscoveryDirectory+"/") {
		return false
	}
	parts := strings.Split(path, "/")
	for depth := 2; depth <= 5 && depth+3 < len(parts); depth++ {
		if parts[depth+1] != managedskills.MaterializationDiscoveryDirectory ||
			(parts[depth] != managedskills.MaterializationWorkspaceDirectory && parts[depth+2] != constants.AgentCLISkillName) {
			continue
		}
		document, known := managedskills.SystemDocument(strings.Join(parts[depth+3:], "/"))
		if !known || document.Bytes != info.Size() || document.Executable != (info.Mode().Perm()&0o111 != 0) {
			continue
		}
		key := strings.Join(parts[:depth], "/")
		if s.systemSkillDirectories == nil {
			s.systemSkillDirectories = make(map[string]map[string]struct{})
		}
		directories, cached := s.systemSkillDirectories[key]
		if !cached {
			directories = s.measureSystemSkillDirectories(owner, parts[:depth])
			s.systemSkillDirectories[key] = directories
		}
		if _, owned := directories[strings.Join(parts[depth:depth+3], "/")]; owned {
			return matchesSystemSkillDocument(s.ctx, root, name, info, document.Digest)
		}
	}
	return false
}

func (s *scanner) measureSystemSkillDirectories(owner *scanRoot, path []string) map[string]struct{} {
	root, err := openConfiguredRoot(owner)
	if err != nil {
		return nil
	}
	defer root.Close()
	directory, err := openMeasuredDirectory(root, path)
	if err != nil {
		return nil
	}
	defer directory.Close()
	workspaceOwner, err := workspace.ReadWorkspaceOwnerAt(directory)
	if err != nil || !s.matchesSystemSkillWorkspace(owner, path, workspaceOwner) {
		return nil
	}
	name := managedskills.MaterializationManifestName
	info, err := directory.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > managedskills.MaterializationManifestMaximumBytes {
		return nil
	}
	file, err := openObservedFile(directory, name, info)
	if err != nil {
		return nil
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(contextReader{s.ctx, file}, managedskills.MaterializationManifestMaximumBytes+1))
	if err != nil || int64(len(data)) != info.Size() || !matchesFileObservation(directory, name, info) {
		return nil
	}
	manifest, err := managedskills.ParseMaterializationManifest(data)
	if err != nil {
		return nil
	}
	directories := make(map[string]struct{})
	for target, entries := range manifest.Targets {
		if digest := entries[constants.AgentCLISkillName]; digest != "" {
			// 原生发现目录的实际副本和快照共用同一份物化记录。
			directories[target+"/"+constants.AgentCLISkillName] = struct{}{}
			directories[managedskills.MaterializationSnapshotPath(digest)] = struct{}{}
		}
	}
	return directories
}

func (s *scanner) matchesSystemSkillWorkspace(owner *scanRoot, path []string, marker workspace.ExecutionWorkspaceOwner) bool {
	if marker.WakerID == "" {
		return false
	}
	if marker.Kind == constants.IsolatedExecutionWorkspaceOwnerKind {
		return marker.ChatID != "" && len(path) == 5 && path[0] == constants.WorkflowWorkspaceDirectoryName &&
			path[2] == constants.WorkflowTemporaryDirectoryName && path[3] == constants.WorkflowWorkerDirectoryName
	}
	if s.systemSkillChatLayout == nil {
		layout, err := chatstorage.NewLayout(owner.path)
		if err != nil {
			return false
		}
		s.systemSkillChatLayout = layout
	}
	var expected string
	var err error
	switch marker.Kind {
	case constants.AgentExecutionWorkspaceOwnerKind:
		expected, err = s.systemSkillChatLayout.AgentWorkspace(marker.WakerID)
	case constants.ThreadMemberExecutionWorkspaceOwnerKind:
		if marker.ChatID == "" || len(path) < 3 || len(path) > 4 {
			return false
		}
		expected, err = s.systemSkillChatLayout.TeamMemberWorkspace(marker.ThreadID, marker.WakerID, len(path) == 3)
	default:
		return false
	}
	return err == nil && expected == filepath.Join(owner.path, filepath.Join(path...))
}

func openConfiguredRoot(configured *scanRoot) (*os.Root, error) {
	if configured.info == nil {
		return nil, fs.ErrNotExist
	}
	root, err := os.OpenRoot(configured.path)
	if err != nil {
		return nil, err
	}
	actual, err := root.Lstat(".")
	if err == nil && os.SameFile(actual, configured.info) {
		return root, nil
	}
	root.Close()
	if err == nil {
		err = fs.ErrInvalid
	}
	return nil, err
}

func openMeasuredDirectory(root *os.Root, path []string) (*os.Root, error) {
	opened := root
	for _, name := range path {
		child, err := openDirectDirectory(opened, name)
		if opened != root {
			opened.Close()
		}
		if err != nil {
			return nil, err
		}
		opened = child
	}
	return opened, nil
}

func openDirectDirectory(root *os.Root, name string) (*os.Root, error) {
	observed, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !observed.IsDir() {
		return nil, fs.ErrInvalid
	}
	child, err := root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	actual, err := child.Lstat(".")
	if err == nil && os.SameFile(observed, actual) {
		return child, nil
	}
	child.Close()
	if err == nil {
		err = fs.ErrInvalid
	}
	return nil, err
}

func openObservedFile(root *os.Root, name string, observed fs.FileInfo) (*os.File, error) {
	file, err := root.OpenFile(name, observeFileFlags, 0)
	if err != nil {
		return nil, err
	}
	actual, err := file.Stat()
	if err == nil && actual.Mode().IsRegular() && os.SameFile(observed, actual) {
		return file, nil
	}
	file.Close()
	if err == nil {
		err = fs.ErrInvalid
	}
	return nil, err
}

func matchesSystemSkillDocument(ctx context.Context, root *os.Root, name string, info fs.FileInfo, expected [sha256.Size]byte) bool {
	file, err := openObservedFile(root, name, info)
	if err != nil {
		return false
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(contextReader{ctx, file}, info.Size()+1))
	if err != nil || size != info.Size() || !matchesFileObservation(root, name, info) {
		return false
	}
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	return digest == expected
}

func matchesFileObservation(root *os.Root, name string, before fs.FileInfo) bool {
	after, err := root.Lstat(name)
	return err == nil && os.SameFile(before, after) && before.Size() == after.Size() &&
		before.Mode() == after.Mode() && before.ModTime().Equal(after.ModTime())
}

type contextReader struct {
	// ctx 在每块读取之前检查扫描生命周期和时间预算。
	ctx context.Context
	// reader 是具有已校验身份的普通文件读取端。
	reader io.Reader
}

func (reader contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}
