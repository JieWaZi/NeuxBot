package workspace

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	managedskills "github.com/JieWaZi/NeuxBot/server/internal/skills"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

const (
	// 暂存放在 Agents 的保留子目录中，与正式工作区位于同一文件系统。
	agentCopyStaging   = constants.WakerCopyStagingDirectoryName
	agentCopyPayload   = "workspace"
	agentCopyWithdrawn = "withdrawn"
	// 原生技能投影在清单提交前写入此类临时目录。
	nativeSkillStagePrefix = ".neuxbot-skills-stage-"
)

// PrepareAgentWorkspaceCopy 校验源空间归属，原子发布独立副本并保留待提交回执。
// 调用者需要独占目标伙伴身份，返回成功后再提交数据库并调用 SettleAgentWorkspaceCopy。
func PrepareAgentWorkspaceCopy(ctx context.Context, sourceRoot, targetRoot, sourceID, targetID string) error {
	agentsRoot := filepath.Dir(targetRoot)
	if !validAgentCopyPath(sourceRoot, sourceID) || !validAgentCopyPath(targetRoot, targetID) ||
		filepath.Dir(sourceRoot) != agentsRoot || strings.EqualFold(sourceID, targetID) {
		return status_error.WorkspacePathInvalid
	}
	return agentCopyError("prepare", targetRoot, prepareAgentWorkspaceCopy(ctx, agentsRoot, sourceID, targetID))
}

func prepareAgentWorkspaceCopy(ctx context.Context, agentsRoot, sourceID, targetID string) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	agents, err := openAgentsForCopy(agentsRoot, true)
	if err != nil {
		return err
	}
	defer agents.Close()
	source, err := openAgentCopyDirectory(agents, sourceID)
	if err != nil {
		return err
	}
	if source != nil {
		defer source.Close()
		if err := validateAgentCopyOwner(source, sourceID, ""); err != nil {
			return err
		}
	}
	if err := requireAgentCopyTargetAbsent(agents, targetID); err != nil {
		return err
	}
	staging, err := createOrOpenAgentCopyDirectory(agents, agentCopyStaging)
	if err != nil {
		return err
	}
	defer staging.Close()
	if err := staging.Mkdir(targetID, 0o700); errors.Is(err, os.ErrExist) {
		return status_error.WorkspaceResourceConflict
	} else if err != nil {
		return err
	}
	defer func() {
		if result != nil {
			// 清理失败时保留回执，启动恢复将重新核对数据库状态。
			_ = agentCopyError("compensate", filepath.Join(agentsRoot, targetID), settleAgentWorkspaceCopy(agents, staging, targetID, false))
		}
	}()
	pending, err := openAgentCopyDirectory(staging, targetID)
	if err != nil {
		return err
	}
	if pending == nil {
		return status_error.ChatDirectoryOwnershipInvalid
	}
	defer pending.Close()
	attemptID := util.New()
	if err := pending.Mkdir(attemptID, 0o700); err != nil {
		return err
	}
	attempt, err := openAgentCopyDirectory(pending, attemptID)
	if err != nil {
		return err
	}
	if attempt == nil {
		return status_error.ChatDirectoryOwnershipInvalid
	}
	defer attempt.Close()
	if err := prepareAgentCopyPayload(ctx, source, attempt, targetID, attemptID); err != nil {
		return err
	}
	for _, directory := range []*os.Root{attempt, pending, staging} {
		if err := syncAgentCopyDirectory(directory, "."); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := requireAgentCopyTargetAbsent(agents, targetID); err != nil {
		return err
	}
	if err := agents.Rename(filepath.Join(agentCopyStaging, targetID, attemptID, agentCopyPayload), targetID); err != nil {
		return err
	}
	return errors.Join(syncAgentCopyDirectory(attempt, "."), syncAgentCopyDirectory(agents, "."), ctx.Err())
}

func prepareAgentCopyPayload(ctx context.Context, source, attempt *os.Root, targetID, attemptID string) error {
	payload, err := createOrOpenAgentCopyDirectory(attempt, agentCopyPayload)
	if err != nil {
		return err
	}
	defer payload.Close()
	if source != nil {
		tree := agentWorkspaceCopy{sourceRoot: source.Name(), destination: payload, links: make(map[string]string)}
		if err := tree.copyDirectory(ctx, source, ".", true); err != nil {
			return err
		}
		if err := tree.copyNativeSkills(ctx, source); err != nil {
			return err
		}
		if err := copyAgentWorkspaceLinks(ctx, payload, tree.links); err != nil {
			return err
		}
	}
	marker := ownerMarker{Kind: constants.AgentExecutionWorkspaceOwnerKind, WakerID: targetID, CopyAttempt: attemptID}
	data, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	file, err := payload.OpenFile(constants.ChatContextOwnerFileName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(data, '\n'))
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	return syncAgentCopyDirectory(payload, ".")
}

type agentWorkspaceCopy struct {
	// sourceRoot 用于检查业务路径并换根源空间内的绝对软链接。
	sourceRoot string
	// destination 是此次尝试专用的暂存副本根目录。
	destination *os.Root
	// links 保存待副本普通文件就绪后再核对的内部软链接。
	links map[string]string
}

func (tree agentWorkspaceCopy) copyDirectory(ctx context.Context, source *os.Root, relative string, filterInternal bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entries, err := fs.ReadDir(source.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := entry.Name()
		// Git 元数据可以引用原仓库的索引和引用，副本只接收工作树文件。
		if strings.EqualFold(name, ".git") {
			continue
		}
		path := filepath.Join(relative, name)
		if filterInternal && (IsInternalExecutionPath(tree.sourceRoot, filepath.Join(tree.sourceRoot, path)) ||
			relative == "." && strings.HasPrefix(name, nativeSkillStagePrefix)) {
			continue
		}
		info, err := source.Lstat(name)
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			child, err := openAgentCopyDirectory(source, name)
			if err != nil {
				return err
			}
			if child == nil {
				return status_error.WorkspaceResourceConflict
			}
			err = tree.destination.Mkdir(path, 0o700)
			if err == nil {
				err = tree.copyDirectory(ctx, child, path, filterInternal)
			}
			child.Close()
			if err != nil {
				return err
			}
		case info.Mode().IsRegular():
			if err := copyAgentWorkspaceFile(ctx, source, tree.destination, name, path, info); err != nil {
				return err
			}
		case info.Mode()&os.ModeSymlink != 0:
			link, err := source.Readlink(name)
			if err != nil {
				return err
			}
			if link = agentCopyLinkTarget(tree.sourceRoot, path, link); link != "" {
				tree.links[path] = link
			}
		}
	}
	return syncAgentCopyDirectory(tree.destination, relative)
}

func (tree agentWorkspaceCopy) copyNativeSkills(ctx context.Context, source *os.Root) error {
	manifest, trusted := readAgentCopySkillManifest(source)
	if !trusted {
		return nil
	}
	entries, err := fs.ReadDir(source.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), nativeSkillStagePrefix) {
			// 中断的安装可能已发布原生文件，旧清单尚不能证明哪些目录属于用户。
			return nil
		}
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := entry.Name()
		if !entry.IsDir() || !isNativeSkillDiscoveryRoot(tree.sourceRoot, name) {
			continue
		}
		if err := tree.copyNativeSkillTarget(ctx, source, name, manifest); err != nil {
			return err
		}
	}
	return nil
}

func (tree agentWorkspaceCopy) copyNativeSkillTarget(ctx context.Context, source *os.Root, directory string, manifest managedskills.MaterializationManifest) error {
	native, err := openAgentCopyDirectory(source, directory)
	if err != nil || native == nil {
		return err
	}
	defer native.Close()
	discoveryName := managedskills.MaterializationDiscoveryDirectory
	info, err := native.Lstat(discoveryName)
	if errors.Is(err, os.ErrNotExist) || err == nil && !info.IsDir() {
		return nil
	}
	if err != nil {
		return err
	}
	discovery, err := openAgentCopyDirectory(native, discoveryName)
	if err != nil || discovery == nil {
		return err
	}
	defer discovery.Close()
	entries, err := fs.ReadDir(discovery.FS(), ".")
	if err != nil {
		return err
	}
	owned := map[string]bool{strings.ToLower(constants.AgentCLISkillName): true}
	key := directory + "/" + discoveryName
	for path, skills := range manifest.Targets {
		if strings.EqualFold(path, key) {
			for name := range skills {
				owned[strings.ToLower(name)] = true
			}
		}
	}
	target := filepath.Join(directory, discoveryName)
	copied := false
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := entry.Name()
		if !entry.IsDir() || !isSafeDirectoryName(name) || owned[strings.ToLower(name)] {
			continue
		}
		skill, err := openAgentCopyDirectory(discovery, name)
		if err != nil {
			return err
		}
		if skill == nil {
			return status_error.WorkspaceResourceConflict
		}
		path := filepath.Join(target, name)
		err = tree.destination.MkdirAll(path, 0o700)
		if err == nil {
			err = tree.copyDirectory(ctx, skill, path, false)
		}
		skill.Close()
		if err != nil {
			return err
		}
		copied = true
	}
	if !copied {
		return nil
	}
	return errors.Join(syncAgentCopyDirectory(tree.destination, target), syncAgentCopyDirectory(tree.destination, directory))
}

func readAgentCopySkillManifest(source *os.Root) (managedskills.MaterializationManifest, bool) {
	name := managedskills.MaterializationManifestName
	info, err := source.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		private, err := openAgentCopyDirectory(source, managedskills.MaterializationWorkspaceDirectory)
		if err != nil || private == nil {
			return managedskills.MaterializationManifest{}, err == nil
		}
		defer private.Close()
		_, err = private.Lstat(managedskills.MaterializationDiscoveryDirectory)
		// 没有清单但已有快照时，原生目录可能包含无法识别的实体投影。
		return managedskills.MaterializationManifest{}, errors.Is(err, os.ErrNotExist)
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > managedskills.MaterializationManifestMaximumBytes {
		return managedskills.MaterializationManifest{}, false
	}
	file, err := source.Open(name)
	if err != nil {
		return managedskills.MaterializationManifest{}, false
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return managedskills.MaterializationManifest{}, false
	}
	data, err := io.ReadAll(io.LimitReader(file, managedskills.MaterializationManifestMaximumBytes+1))
	if err != nil {
		return managedskills.MaterializationManifest{}, false
	}
	manifest, err := managedskills.ParseMaterializationManifest(data)
	return manifest, err == nil
}

type agentCopyReader struct {
	// ctx 允许长文件复制在分块读取之间及时停止。
	ctx context.Context
	// file 是已核对身份的源文件句柄。
	file *os.File
}

func (reader agentCopyReader) Read(data []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.file.Read(data)
}

func copyAgentWorkspaceFile(ctx context.Context, source, destination *os.Root, name, path string, info os.FileInfo) (result error) {
	input, err := source.Open(name)
	if err != nil {
		return err
	}
	defer input.Close()
	actual, err := input.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(info, actual) {
		return status_error.WorkspaceResourceConflict
	}
	mode := os.FileMode(0o600) | actual.Mode().Perm()&0o100
	output, err := destination.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, output.Close()) }()
	size, err := io.Copy(output, agentCopyReader{ctx: ctx, file: input})
	if err != nil {
		return err
	}
	completed, err := input.Stat()
	if err != nil {
		return err
	}
	if size != actual.Size() || completed.Size() != size || !actual.ModTime().Equal(completed.ModTime()) {
		return status_error.WorkspaceResourceConflict
	}
	return output.Sync()
}

func agentCopyLinkTarget(sourceRoot, path, link string) string {
	if filepath.IsAbs(link) {
		// 绝对链接只重定向到同一路径下的副本，带回退组件的原路径无法按词法安全换根。
		if link != filepath.Clean(link) {
			return ""
		}
		local, err := filepath.Rel(sourceRoot, link)
		if err != nil || local != "." && !filepath.IsLocal(local) {
			return ""
		}
		link, _ = filepath.Rel(filepath.Dir(path), local)
	}
	if link == "" || filepath.VolumeName(link) != "" || os.IsPathSeparator(link[0]) {
		return ""
	}
	return link
}

func copyAgentWorkspaceLinks(ctx context.Context, destination *os.Root, links map[string]string) error {
	for len(links) != 0 {
		created := false
		for name, link := range links {
			if err := ctx.Err(); err != nil {
				return err
			}
			parent := filepath.Dir(name)
			// 在暂存树内逐项核验实际目标，拒绝越界、内部配置和没有复制入口的链接。
			if _, err := destination.Stat(parent + string(filepath.Separator) + link); err != nil {
				continue
			}
			if err := destination.Symlink(link, name); err != nil {
				return err
			}
			if err := syncAgentCopyDirectory(destination, parent); err != nil {
				return err
			}
			delete(links, name)
			created = true
		}
		if !created {
			break
		}
	}
	return nil
}

// SettleAgentWorkspaceCopy 按已确认的数据库结果保留副本，或撤回此次尝试发布的空间。
func SettleAgentWorkspaceCopy(targetRoot, targetID string, committed bool) error {
	if !validAgentCopyPath(targetRoot, targetID) {
		return status_error.WorkspacePathInvalid
	}
	err := withAgentCopyStaging(filepath.Dir(targetRoot), func(agents, staging *os.Root) error {
		return settleAgentWorkspaceCopy(agents, staging, targetID, committed)
	})
	return agentCopyError("settle", targetRoot, err)
}

// RecoverAgentWorkspaceCopies 在启动开放请求前，按数据库内的伙伴身份核对未结算复制。
func RecoverAgentWorkspaceCopies(ctx context.Context, agentsRoot string, exists func(context.Context, string) (bool, error)) error {
	if !filepath.IsAbs(agentsRoot) || filepath.Clean(agentsRoot) != agentsRoot || exists == nil {
		return status_error.WorkspacePathInvalid
	}
	err := withAgentCopyStaging(agentsRoot, func(agents, staging *os.Root) error {
		entries, err := fs.ReadDir(staging.FS(), ".")
		if err != nil {
			return err
		}
		var recoveryErr error
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			id := entry.Name()
			if !entry.IsDir() || !validAgentCopyID(id) {
				continue
			}
			committed, err := exists(ctx, id)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err == nil {
				err = settleAgentWorkspaceCopy(agents, staging, id, committed)
			}
			if err != nil {
				recoveryErr = agentCopyError("recover", filepath.Join(agentsRoot, id), err)
			}
		}
		return recoveryErr
	})
	return agentCopyError("scan", agentsRoot, err)
}

func settleAgentWorkspaceCopy(agents, staging *os.Root, targetID string, committed bool) error {
	pending, err := openAgentCopyDirectory(staging, targetID)
	if err != nil || pending == nil {
		return err
	}
	err = finishAgentCopyAttempt(agents, pending, targetID, committed)
	if err := errors.Join(err, pending.Close()); err != nil {
		return err
	}
	if err := staging.Remove(targetID); err != nil {
		return err
	}
	return syncAgentCopyDirectory(staging, ".")
}

func finishAgentCopyAttempt(agents, pending *os.Root, targetID string, committed bool) error {
	entries, err := fs.ReadDir(pending.FS(), ".")
	if err != nil || len(entries) == 0 {
		return err
	}
	attemptID := entries[0].Name()
	if len(entries) != 1 || !entries[0].IsDir() || !validAgentCopyAttempt(attemptID) {
		return status_error.ChatDirectoryOwnershipInvalid
	}
	attempt, err := openAgentCopyDirectory(pending, attemptID)
	if err != nil {
		return err
	}
	if attempt == nil {
		return status_error.ChatDirectoryOwnershipInvalid
	}
	err = reconcileAgentCopyPayload(agents, attempt, targetID, attemptID, committed)
	if err := errors.Join(err, attempt.Close()); err != nil {
		return err
	}
	if err := pending.Remove(attemptID); err != nil {
		return err
	}
	return syncAgentCopyDirectory(pending, ".")
}

func reconcileAgentCopyPayload(agents, attempt *os.Root, targetID, attemptID string, committed bool) error {
	withdrawn, err := openAgentCopyDirectory(attempt, agentCopyWithdrawn)
	if err != nil {
		return err
	}
	if withdrawn != nil {
		withdrawn.Close()
		if committed {
			return status_error.ChatDirectoryOwnershipInvalid
		}
		return discardWithdrawnAgentCopy(agents, attempt, targetID, attemptID)
	}
	payload, err := openAgentCopyDirectory(attempt, agentCopyPayload)
	if err != nil {
		return err
	}
	if payload != nil {
		payload.Close()
	}
	if committed || payload == nil {
		target, err := openAgentCopyDirectory(agents, targetID)
		if err != nil {
			return err
		}
		if target != nil {
			err = validateAgentCopyOwner(target, targetID, attemptID)
			target.Close()
			if err != nil {
				return err
			}
		} else if committed {
			return status_error.ChatDirectoryOwnershipInvalid
		}
		if !committed && target != nil {
			path := filepath.Join(agentCopyStaging, targetID, attemptID, agentCopyWithdrawn)
			if err := agents.Rename(targetID, path); err != nil {
				return err
			}
			if err := errors.Join(syncAgentCopyDirectory(attempt, "."), syncAgentCopyDirectory(agents, ".")); err != nil {
				return err
			}
			return discardWithdrawnAgentCopy(agents, attempt, targetID, attemptID)
		}
	}
	return attempt.RemoveAll(agentCopyPayload)
}

func discardWithdrawnAgentCopy(agents, attempt *os.Root, targetID, attemptID string) error {
	withdrawn, err := openAgentCopyDirectory(attempt, agentCopyWithdrawn)
	if err != nil {
		return err
	}
	if withdrawn == nil {
		return status_error.ChatDirectoryOwnershipInvalid
	}
	err = validateAgentCopyOwner(withdrawn, targetID, attemptID)
	withdrawn.Close()
	if err != nil {
		// 检查和重命名之间被替换的目录仍属于原使用者，不能作为副本清理。
		if requireAgentCopyTargetAbsent(agents, targetID) == nil {
			path := filepath.Join(agentCopyStaging, targetID, attemptID, agentCopyWithdrawn)
			restoreErr := agents.Rename(path, targetID)
			if restoreErr == nil {
				restoreErr = errors.Join(syncAgentCopyDirectory(attempt, "."), syncAgentCopyDirectory(agents, "."))
			}
			_ = agentCopyError("restore_replaced_target", filepath.Join(agents.Name(), targetID), restoreErr)
		}
		return err
	}
	if err := requireAgentCopyTargetAbsent(attempt, agentCopyPayload); err != nil {
		return err
	}
	// 撤回身份确认后转入只需清理的状态，回收中断也不依赖尚未删除的归属文件。
	if err := attempt.Rename(agentCopyWithdrawn, agentCopyPayload); err != nil {
		return err
	}
	if err := syncAgentCopyDirectory(attempt, "."); err != nil {
		return err
	}
	return attempt.RemoveAll(agentCopyPayload)
}

func validAgentCopyID(id string) bool {
	return isSafeDirectoryName(id) && filepath.IsLocal(id) && !strings.EqualFold(id, agentCopyStaging)
}

func validAgentCopyPath(path, id string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && filepath.Base(path) == id && validAgentCopyID(id)
}

func validAgentCopyAttempt(attempt string) bool {
	decoded, err := hex.DecodeString(attempt)
	return err == nil && len(decoded) == 16
}

func validateAgentCopyOwner(directory *os.Root, targetID, attemptID string) error {
	owner, err := readWorkspaceOwnerMarkerAt(directory)
	if err != nil {
		return err
	}
	if owner.Kind != constants.AgentExecutionWorkspaceOwnerKind || owner.WakerID != targetID ||
		attemptID != "" && owner.CopyAttempt != attemptID {
		return status_error.ChatDirectoryOwnershipInvalid
	}
	return nil
}

func withAgentCopyStaging(agentsRoot string, fn func(agents, staging *os.Root) error) error {
	agents, err := openAgentsForCopy(agentsRoot, false)
	if err != nil || agents == nil {
		return err
	}
	defer agents.Close()
	staging, err := openAgentCopyDirectory(agents, agentCopyStaging)
	if err != nil || staging == nil {
		return err
	}
	defer staging.Close()
	return fn(agents, staging)
}

func openAgentsForCopy(agentsRoot string, create bool) (*os.Root, error) {
	managed, err := os.OpenRoot(filepath.Dir(agentsRoot))
	if !create && errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer managed.Close()
	if create {
		return createOrOpenAgentCopyDirectory(managed, filepath.Base(agentsRoot))
	}
	return openAgentCopyDirectory(managed, filepath.Base(agentsRoot))
}

func createOrOpenAgentCopyDirectory(parent *os.Root, name string) (*os.Root, error) {
	if err := parent.Mkdir(name, 0o700); err == nil {
		if err := syncAgentCopyDirectory(parent, "."); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	opened, err := openAgentCopyDirectory(parent, name)
	if err == nil && opened == nil {
		return nil, status_error.ChatDirectoryOwnershipInvalid
	}
	return opened, err
}

func openAgentCopyDirectory(parent *os.Root, name string) (*os.Root, error) {
	info, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, status_error.ChatDirectoryOwnershipInvalid
	}
	opened, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	actual, err := opened.Stat(".")
	if err != nil || !os.SameFile(info, actual) {
		opened.Close()
		return nil, status_error.ChatDirectoryOwnershipInvalid
	}
	return opened, nil
}

func requireAgentCopyTargetAbsent(agents *os.Root, targetID string) error {
	_, err := agents.Lstat(targetID)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err == nil {
		return status_error.WorkspaceResourceConflict
	}
	return err
}

func syncAgentCopyDirectory(root *os.Root, path string) error {
	// Windows 的普通目录句柄不支持 FlushFileBuffers，目录发布仍使用同卷重命名。
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := root.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

func agentCopyError(operation, path string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return status_error.OperationCanceled
	}
	var status status_error.StatusError
	if errors.As(err, &status) {
		return status
	}
	logx.Errorf("agent workspace copy failed: operation=%s path=%q error=%v", operation, path, err)
	return status_error.ManagedWorkspaceError
}
