package changes

import (
	"context"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/format/idxfile"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/gofrs/flock"
)

// Discard 丢弃调用方尚未发布的采集，不保留用于历史展示的 incomplete 记录。
// 调用方必须确认没有 JSONL 引用；已完成的 ready/partial 记录不能通过此入口删除。
// 本方法只删除当前记录，其他活动采集的耐久基线仍参与对象保留计算。
func (c *Capture) Discard(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, c.options.Limits.Timeout)
	defer cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.withLock(ctx, func() error {
		if !c.discarded {
			r, err := loadRecord(c.owned, c.record.Summary.ID)
			if err != nil && err != status_error.ChatMessageFileChangeNotFound {
				return err
			}
			if r != nil && (r.Summary.State == "ready" || r.Summary.State == "partial") {
				return status_error.WorkspaceResourceConflict
			}
			if r != nil && c.lease == nil {
				lease := flock.New(filepath.Join(filepath.Dir(recordPath(c.owned, c.record.Summary.ID)), "capture.lock"))
				available, err := lease.TryLock()
				if err != nil {
					lease.Close()
					return storageError("discard_lease", err)
				}
				if !available {
					lease.Close()
					return status_error.WorkspaceResourceConflict
				}
				c.lease = lease
			}
			c.closed = true
			c.releaseLease()
			root, err := os.OpenRoot(c.owned)
			if err != nil {
				return storageError("discard_root", err)
			}
			err = root.RemoveAll(filepath.Join("changes", "records", c.record.Summary.ID))
			root.Close()
			if err != nil {
				return storageError("discard_record", err)
			}
			c.discarded = true
			if err := syncDir(filepath.Join(base(c.owned), "records")); err != nil {
				return storageError("discard_record_sync", err)
			}
		}
		directory, err := os.Open(filepath.Join(base(c.owned), "records"))
		if err != nil {
			return storageError("discard_records", err)
		}
		defer directory.Close()
		records := []*record{}
		for {
			entries, readErr := directory.ReadDir(128)
			for _, entry := range entries {
				if ctx.Err() != nil {
					return status_error.OperationCanceled
				}
				if !entry.IsDir() || !validID(entry.Name()) {
					continue
				}
				r, err := loadRecord(c.owned, entry.Name())
				if err == status_error.ChatMessageFileChangeNotFound {
					continue
				}
				if err != nil {
					return err
				}
				// Git tree 已闭合包含本阶段全部 blob，只保留树根，避免累积历史大清单。
				references := &record{}
				for _, value := range r.Before {
					references.Before = append(references.Before, snapshot{Tree: value.Tree})
				}
				for _, value := range r.After {
					references.After = append(references.After, snapshot{Tree: value.Tree})
				}
				records = append(records, references)
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				return storageError("discard_records", readErr)
			}
		}
		return collectGarbage(ctx, c.owned, records)
	})
}

// CloneRecord 在目标日志引用提交前复制单条记录及其对象闭包。
// 目标不依赖来源目录、用户项目目录或来源 Chat 的后续生命周期。
func CloneRecord(sourceDir, targetDir, recordID string) error {
	if !validID(recordID) || !filepath.IsAbs(sourceDir) || !filepath.IsAbs(targetDir) {
		return status_error.WorkspacePathInvalid
	}
	source, err := filepath.EvalSymlinks(sourceDir)
	if err != nil {
		return storageError("clone_source", err)
	}
	target, err := prepareOwned(targetDir)
	if err != nil {
		return storageError("clone_target", err)
	}
	if source == target {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	paths := []string{source, target}
	sort.Strings(paths)
	locks := []*flock.Flock{}
	defer func() {
		for i := len(locks) - 1; i >= 0; i-- {
			locks[i].Unlock()
			locks[i].Close()
		}
	}()
	for _, path := range paths {
		lock := flock.New(filepath.Join(base(path), "write.lock"))
		ok, err := lock.TryLockContext(ctx, 10*time.Millisecond)
		if err != nil || !ok {
			lock.Close()
			return status_error.OperationCanceled
		}
		locks = append(locks, lock)
	}
	r, err := loadRecord(source, recordID)
	if err != nil {
		return err
	}
	if r.Summary.State == "pending" {
		return status_error.WorkspaceResourceConflict
	}
	if _, err := os.Stat(recordPath(target, recordID)); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return storageError("clone_target_record", err)
	}
	sourceRoot, err := os.OpenRoot(source)
	if err != nil {
		return storageError("clone_root", err)
	}
	defer sourceRoot.Close()
	destination := filepath.Dir(recordPath(target, recordID))
	if err := os.MkdirAll(destination, 0700); err != nil {
		return storageError("clone_directory", err)
	}
	if err := syncDir(filepath.Dir(destination)); err != nil {
		return storageError("clone_record_directory", err)
	}
	// 对象先完整复制，record.json 最后原子提交，崩溃只会留下不可见的暂存内容。
	if len(r.Before) > 0 || len(r.After) > 0 {
		if _, err := os.Stat(repositoryPath(target)); os.IsNotExist(err) {
			if _, err := git.PlainInit(repositoryPath(target), true); err != nil {
				return storageError("clone_repository", err)
			}
			if err := syncRepositoryLayout(target); err != nil {
				return storageError("clone_repository_sync", err)
			}
		} else if err != nil {
			return storageError("clone_repository", err)
		}
		needed := map[string]bool{}
		sourceStore := objectStore(source)
		var addTree func(string) error
		addTree = func(hash string) error {
			if hash == "" || needed[hash] {
				return nil
			}
			needed[hash] = true
			tree, err := object.GetTree(sourceStore, plumbing.NewHash(hash))
			if err != nil {
				return storageError("clone_tree", err)
			}
			for _, entry := range tree.Entries {
				if entry.Mode == filemode.Dir {
					if err := addTree(entry.Hash.String()); err != nil {
						return err
					}
				} else {
					needed[entry.Hash.String()] = true
				}
			}
			return nil
		}
		for _, snapshot := range append(append([]snapshot{}, r.Before...), r.After...) {
			if err := addTree(snapshot.Tree); err != nil {
				return err
			}
			for _, entry := range snapshot.Entries {
				if entry.Blob != "" {
					needed[entry.Blob] = true
				}
			}
		}
		usage := int64(0)
		targetStore := objectStore(target)
		iterator, err := targetStore.IterEncodedObjects(plumbing.AnyObject)
		if err != nil {
			return storageError("clone_usage", err)
		}
		err = iterator.ForEach(func(value plumbing.EncodedObject) error { usage += value.Size(); return nil })
		iterator.Close()
		if err != nil {
			return storageError("clone_usage", err)
		}
		missing := []plumbing.Hash{}
		for hash := range needed {
			if ctx.Err() != nil {
				return status_error.OperationCanceled
			}
			if len(hash) != 40 {
				return status_error.WorkspaceScopeInvalid
			}
			if targetStore.HasEncodedObject(plumbing.NewHash(hash)) == nil {
				continue
			}
			encoded, err := sourceStore.EncodedObject(plumbing.AnyObject, plumbing.NewHash(hash))
			if err != nil {
				return storageError("clone_object_size", err)
			}
			if usage+encoded.Size() > normalizedLimits(Limits{}).MaxChatBytes {
				return status_error.WorkspaceResourceTooLarge
			}
			usage += encoded.Size()
			missing = append(missing, plumbing.NewHash(hash))
		}
		if err := clonePack(ctx, sourceStore, target, missing); err != nil {
			return err
		}
	}
	if len(r.Summary.Files) > 0 {
		diffs := filepath.Join(destination, "diffs")
		if err := os.MkdirAll(diffs, 0700); err != nil {
			return storageError("clone_diff_directory", err)
		}
		for _, file := range r.Summary.Files {
			name := identity(file.RootID+"/"+file.Path) + ".json"
			if err := copyOwnedFile(sourceRoot, filepath.Join("changes", "records", recordID, "diffs", name), filepath.Join(diffs, name)); err != nil {
				return storageError("clone_diff", err)
			}
		}
	}
	return saveRecord(target, r)
}

type contextWriter struct {
	// ctx 限制对象包写入和复制的时间。
	ctx context.Context
	// target 保存原始字节，不解释文件内容。
	target io.Writer
}

func (writer contextWriter) Write(data []byte) (int, error) {
	if writer.ctx.Err() != nil {
		return 0, status_error.OperationCanceled
	}
	return writer.target.Write(data)
}

func clonePack(ctx context.Context, source *filesystem.Storage, target string, hashes []plumbing.Hash) error {
	if len(hashes) == 0 {
		return nil
	}
	sort.Slice(hashes, func(i, j int) bool { return hashes[i].String() < hashes[j].String() })
	directory := filepath.Join(repositoryPath(target), "objects", "pack")
	file, err := os.CreateTemp(directory, "pending-pack-")
	if err != nil {
		return storageError("clone_pack", err)
	}
	name := file.Name()
	defer os.Remove(name)
	defer file.Close()
	hash, err := packfile.NewEncoder(contextWriter{ctx: ctx, target: file}, source, false).Encode(hashes, 0)
	if err != nil {
		return storageError("clone_pack_encode", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return storageError("clone_pack_seek", err)
	}
	observer := new(idxfile.Writer)
	parser, err := packfile.NewParser(packfile.NewScanner(file), observer)
	if err != nil {
		return storageError("clone_pack_index", err)
	}
	if _, err := parser.Parse(); err != nil {
		return storageError("clone_pack_index", err)
	}
	index, err := observer.Index()
	if err != nil {
		return storageError("clone_pack_index", err)
	}
	indexFile, err := os.CreateTemp(directory, "pending-index-")
	if err != nil {
		return storageError("clone_pack_index", err)
	}
	indexName := indexFile.Name()
	defer os.Remove(indexName)
	defer indexFile.Close()
	if _, err := idxfile.NewEncoder(contextWriter{ctx: ctx, target: indexFile}).Encode(index); err != nil {
		return storageError("clone_pack_index", err)
	}
	if err := file.Sync(); err != nil {
		return storageError("clone_pack_sync", err)
	}
	if err := indexFile.Sync(); err != nil {
		return storageError("clone_pack_sync", err)
	}
	if err := file.Close(); err != nil {
		return storageError("clone_pack_close", err)
	}
	if err := indexFile.Close(); err != nil {
		return storageError("clone_pack_close", err)
	}
	stem := filepath.Join(directory, "pack-"+hash.String())
	if err := os.Rename(name, stem+".pack"); err != nil {
		return storageError("clone_pack_commit", err)
	}
	if err := os.Rename(indexName, stem+".idx"); err != nil {
		return storageError("clone_pack_commit", err)
	}
	if err := syncDir(directory); err != nil {
		return storageError("clone_pack_sync", err)
	}
	return nil
}

func copyOwnedFile(source *os.Root, relative, target string) error {
	info, err := source.Lstat(relative)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return status_error.WorkspaceScopeInvalid
	}
	input, err := openRegular(source, relative, info)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	output, err := os.CreateTemp(filepath.Dir(target), "pending-")
	if err != nil {
		return err
	}
	name := output.Name()
	defer os.Remove(name)
	err = output.Chmod(0600)
	if err == nil {
		_, err = io.Copy(output, input)
	}
	if err == nil {
		err = output.Sync()
	}
	closeErr := output.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(name, target); err != nil {
		return err
	}
	return syncDir(filepath.Dir(target))
}

func cleanupScratch(owned, id string) error {
	directory := filepath.Dir(recordPath(owned, id))
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "pending-source-") {
			root, err := os.OpenRoot(directory)
			if err != nil {
				return err
			}
			err = root.RemoveAll(entry.Name())
			root.Close()
			if err != nil {
				return err
			}
			continue
		}
		if entry.Type()&fs.ModeSymlink != 0 || entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), "pending-") || strings.HasSuffix(entry.Name(), ".index") || strings.HasSuffix(entry.Name(), ".index.lock") {
			if err := os.Remove(filepath.Join(directory, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// collectGarbage 仅在没有活动采集的恢复阶段删除无耐久记录引用的私有对象。
func collectGarbage(ctx context.Context, owned string, records []*record) error {
	if _, err := os.Stat(repositoryPath(owned)); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return storageError("recover_objects", err)
	}
	store := objectStore(owned)
	keep := map[string]bool{}
	var mark func(string) error
	mark = func(hash string) error {
		if hash == "" || keep[hash] {
			return nil
		}
		if ctx.Err() != nil {
			return status_error.OperationCanceled
		}
		keep[hash] = true
		tree, err := object.GetTree(store, plumbing.NewHash(hash))
		if err != nil {
			return storageError("recover_tree", err)
		}
		for _, entry := range tree.Entries {
			if entry.Mode == filemode.Dir {
				if err := mark(entry.Hash.String()); err != nil {
					return err
				}
			} else {
				keep[entry.Hash.String()] = true
			}
		}
		return nil
	}
	for _, r := range records {
		for _, snapshot := range append(append([]snapshot{}, r.Before...), r.After...) {
			if err := mark(snapshot.Tree); err != nil {
				return err
			}
			for _, entry := range snapshot.Entries {
				if entry.Blob != "" {
					keep[entry.Blob] = true
				}
			}
		}
	}
	root, err := os.OpenRoot(owned)
	if err != nil {
		return storageError("recover_objects_root", err)
	}
	defer root.Close()
	objects := filepath.Join("changes", "repository.git", "objects")
	buckets, err := os.ReadDir(filepath.Join(owned, objects))
	if err != nil {
		return storageError("recover_objects_list", err)
	}
	for _, bucket := range buckets {
		if !bucket.IsDir() || len(bucket.Name()) != 2 {
			continue
		}
		if _, err := hex.DecodeString(bucket.Name()); err != nil {
			continue
		}
		directory := filepath.Join(objects, bucket.Name())
		handle, err := root.Open(directory)
		if err != nil {
			return storageError("recover_object_bucket", err)
		}
		changed := false
		for {
			entries, readErr := handle.ReadDir(1024)
			for _, entry := range entries {
				if ctx.Err() != nil {
					handle.Close()
					return status_error.OperationCanceled
				}
				hash := bucket.Name() + entry.Name()
				if len(hash) != 40 || keep[hash] || !entry.Type().IsRegular() {
					continue
				}
				if _, err := hex.DecodeString(hash); err != nil {
					continue
				}
				if err := root.Remove(filepath.Join(directory, entry.Name())); err != nil {
					handle.Close()
					return storageError("recover_orphan_object", err)
				}
				changed = true
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				handle.Close()
				return storageError("recover_object_bucket", readErr)
			}
		}
		handle.Close()
		if changed {
			if err := syncDir(filepath.Join(owned, directory)); err != nil {
				return storageError("recover_object_bucket_sync", err)
			}
		}
	}
	pack := filepath.Join(objects, "pack")
	entries, err := os.ReadDir(filepath.Join(owned, pack))
	if err != nil && !os.IsNotExist(err) {
		return storageError("recover_object_staging", err)
	}
	indexes := map[string]bool{}
	changed := false
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !strings.HasPrefix(name, "pack-") || !strings.HasSuffix(name, ".idx") || len(name) != 49 {
			continue
		}
		stem := strings.TrimSuffix(name, ".idx")
		indexes[stem] = true
		file, err := root.Open(filepath.Join(pack, name))
		if err != nil {
			return storageError("recover_pack_index", err)
		}
		index := new(idxfile.MemoryIndex)
		err = idxfile.NewDecoder(io.LimitReader(file, 8<<20)).Decode(index)
		file.Close()
		if err != nil {
			return storageError("recover_pack_index", err)
		}
		iterator, err := index.Entries()
		if err != nil {
			return storageError("recover_pack_index", err)
		}
		referenced := false
		for {
			if ctx.Err() != nil {
				iterator.Close()
				return status_error.OperationCanceled
			}
			entry, err := iterator.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				iterator.Close()
				return storageError("recover_pack_index", err)
			}
			if keep[entry.Hash.String()] {
				referenced = true
				break
			}
		}
		iterator.Close()
		if !referenced {
			for _, suffix := range []string{".idx", ".pack", ".keep"} {
				if err := root.Remove(filepath.Join(pack, stem+suffix)); err != nil && !os.IsNotExist(err) {
					return storageError("recover_orphan_pack", err)
				}
			}
			changed = true
		}
	}
	for _, entry := range entries {
		name := entry.Name()
		orphan := strings.HasPrefix(name, "pack-") && strings.HasSuffix(name, ".pack") && len(name) == 50 && !indexes[strings.TrimSuffix(name, ".pack")]
		if entry.Type().IsRegular() && (strings.HasPrefix(name, "tmp_obj_") || strings.HasPrefix(name, "tmp_pack_") || strings.HasPrefix(name, "tmp_idx_") || strings.HasPrefix(name, "pending-pack-") || strings.HasPrefix(name, "pending-index-") || orphan) {
			if err := root.Remove(filepath.Join(pack, entry.Name())); err != nil {
				return storageError("recover_object_staging", err)
			}
			changed = true
		}
	}
	if changed {
		if err := syncDir(filepath.Join(owned, pack)); err != nil {
			return storageError("recover_pack_sync", err)
		}
	}
	return nil
}
