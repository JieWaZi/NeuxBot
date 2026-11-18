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
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	managedskills "github.com/JieWaZi/NeuxBot/server/internal/skills"
)

const maxMeasuredSkillFileBytes = 50 << 20

type skillFileFingerprint struct {
	// digest 是源或副本文件的内容摘要。
	digest [sha256.Size]byte
	// size 是摘要对应的实际文件字节数。
	size int64
	// executable 区分运行时必须保持的执行位。
	executable bool
}

type skillMeasurements struct {
	// manifests 复用一次扫描已读取的伙伴物化记录，空记录表示无法核验。
	manifests map[string]managedskills.MaterializationManifest
	// sourceFiles 缓存已核验的不可变源文件，避免多伙伴副本重复读取相同源内容。
	sourceFiles map[string]skillFileFingerprint
}

func (s *scanner) isSystemSkillCopy(owner *scanRoot, root *os.Root, relative, name string, info fs.FileInfo) bool {
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) < 6 || parts[3] != "skills" || parts[4] != constants.AgentCLISkillName || info.Size() > maxMeasuredSkillFileBytes {
		return false
	}
	partnerDirectory := filepath.Join(owner.path, parts[0], parts[1])
	if s.skills.manifests == nil {
		s.skills.manifests = make(map[string]managedskills.MaterializationManifest)
		s.skills.sourceFiles = make(map[string]skillFileFingerprint)
	}
	manifest, cached := s.skills.manifests[partnerDirectory]
	if !cached {
		manifest = s.measureSkillManifest(owner, parts[:2])
		s.skills.manifests[partnerDirectory] = manifest
	}
	version := manifest.Targets[parts[2]+"/"+parts[3]][constants.AgentCLISkillName]
	if version == "" {
		return false
	}
	sourcePath := append([]string{"system-skills", version}, parts[5:]...)
	key := strings.Join(sourcePath, "/")
	source, cached := s.skills.sourceFiles[key]
	if !cached {
		var ok bool
		source, ok = s.measureSkillSource(sourcePath)
		if !ok {
			return false
		}
		s.skills.sourceFiles[key] = source
	}
	if source.size != info.Size() || source.executable != (info.Mode().Perm()&0o111 != 0) {
		return false
	}
	copy, err := fingerprintSkillFile(s.ctx, root, name, info)
	return err == nil && copy == source
}

func (s *scanner) measureSkillManifest(owner *scanRoot, path []string) managedskills.MaterializationManifest {
	empty := managedskills.MaterializationManifest{}
	root, err := openConfiguredRoot(owner)
	if err != nil {
		return empty
	}
	defer root.Close()
	directory, err := openMeasuredDirectory(root, path)
	if err != nil {
		return empty
	}
	defer directory.Close()
	name := managedskills.MaterializationManifestName
	info, err := directory.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > managedskills.MaterializationManifestMaximumBytes {
		return empty
	}
	file, err := openObservedFile(directory, name, info)
	if err != nil {
		return empty
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(contextReader{s.ctx, file}, managedskills.MaterializationManifestMaximumBytes+1))
	if err != nil || int64(len(data)) != info.Size() || !matchesFileObservation(directory, name, info) {
		return empty
	}
	manifest, _ := managedskills.ParseMaterializationManifest(data)
	return manifest
}

func (s *scanner) measureSkillSource(path []string) (skillFileFingerprint, bool) {
	for _, configured := range s.roots {
		if configured.scope&dataRoot == 0 {
			continue
		}
		root, err := openConfiguredRoot(configured)
		if err != nil {
			break
		}
		defer root.Close()
		directory, err := openMeasuredDirectory(root, path[:len(path)-1])
		if err != nil {
			break
		}
		defer directory.Close()
		name := path[len(path)-1]
		info, err := directory.Lstat(name)
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxMeasuredSkillFileBytes {
			break
		}
		value, err := fingerprintSkillFile(s.ctx, directory, name, info)
		return value, err == nil
	}
	return skillFileFingerprint{}, false
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

func fingerprintSkillFile(ctx context.Context, root *os.Root, name string, info fs.FileInfo) (skillFileFingerprint, error) {
	file, err := openObservedFile(root, name, info)
	if err != nil {
		return skillFileFingerprint{}, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(contextReader{ctx, file}, info.Size()+1))
	if err != nil {
		return skillFileFingerprint{}, err
	}
	if size != info.Size() || !matchesFileObservation(root, name, info) {
		return skillFileFingerprint{}, status_error.SkillContentInvalid
	}
	fingerprint := skillFileFingerprint{size: size, executable: info.Mode().Perm()&0o111 != 0}
	copy(fingerprint.digest[:], hash.Sum(nil))
	return fingerprint, nil
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
