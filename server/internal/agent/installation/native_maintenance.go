package installation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type nativeOwner struct {
	// RuntimeID binds the selected native file to one official provider.
	RuntimeID string
	// Path is the exact regular file maintained in place; shims are never rewritten.
	Path string
	// Digest is the verified current native file's SHA256.
	Digest string
	// Version is the publisher version that established the file identity.
	Version string
	// SourceURL is the official artifact identity that established ownership.
	SourceURL string
}

// Only self-contained publisher artifacts can be replaced as one file. Bundled
// Codex/Cursor/Pi and interpreter scripts must retain their original owner/layout.
var singleFileProviders = map[string]bool{"claude": true, "kimi": true, "opencode": true, "grok": true, "qoder": true, "qoder-cn": true, "workbuddy": true}

func nativeImage(name string) bool {
	file, err := os.Open(name)
	if err != nil {
		return false
	}
	defer file.Close()
	var header [4]byte
	if _, err := io.ReadFull(file, header[:]); err != nil {
		return false
	}
	if header[0] == 'M' && header[1] == 'Z' || string(header[:]) == "\x7fELF" {
		return true
	}
	for _, magic := range []string{"\xcf\xfa\xed\xfe", "\xfe\xed\xfa\xcf", "\xce\xfa\xed\xfe", "\xfe\xed\xfa\xce", "\xca\xfe\xba\xbe", "\xbe\xba\xfe\xca"} {
		if string(header[:]) == magic {
			return true
		}
	}
	return false
}

func (i *Installer) nativeReceiptPath(id, path string) string {
	digest := sha256.Sum256([]byte(filepath.Clean(path)))
	return filepath.Join(i.root, ".external-receipts", id+"-"+hex.EncodeToString(digest[:])+".json")
}

func (i *Installer) nativeOwnership(id, binary string, args []string) *nativeOwner {
	if !singleFileProviders[id] || len(args) != 0 {
		return nil
	}
	path := resolvedFile(binary)
	if path == "" || inside(i.root, path) {
		return nil
	}
	for _, marker := range []string{"/Cellar/", "/Caskroom/", "/node_modules/", "/pipx/", "/.venv/", "/uv/tools/"} {
		if strings.Contains(filepath.ToSlash(path), marker) {
			return nil
		}
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || !nativeImage(path) {
		return nil
	}
	digest, err := digestFile(path)
	if err != nil {
		return nil
	}
	receiptDirectory := filepath.Join(i.root, ".external-receipts")
	rootInfo, rootErr := os.Lstat(i.root)
	receiptInfo, receiptErr := os.Lstat(receiptDirectory)
	canReadReceipt := rootErr == nil && rootInfo.IsDir() && rootInfo.Mode()&os.ModeSymlink == 0 && receiptErr == nil && receiptInfo.IsDir() && receiptInfo.Mode()&os.ModeSymlink == 0
	if canReadReceipt {
		if doc, ok := readLocalJSON(i.nativeReceiptPath(id, path)); ok {
			data, _ := json.Marshal(doc)
			var receipt nativeOwner
			if json.Unmarshal(data, &receipt) == nil && receipt.RuntimeID == id && samePath(receipt.Path, path) && receipt.Digest == digest && versionPattern.MatchString(receipt.Version) {
				parsed, err := url.Parse(receipt.SourceURL)
				if err == nil && officialURL(parsed) {
					return &receipt
				}
			}
		}
	}
	if i.validateStore(id) != nil {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(i.root, id))
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "version-") {
			continue
		}
		version, err := i.readManagedVersion(id, filepath.Join(i.root, id, entry.Name()))
		if err != nil || version.record.Node || len(version.record.Args) != 0 || version.record.Digests[version.record.Binary] != digest {
			continue
		}
		return &nativeOwner{RuntimeID: id, Path: path, Digest: digest, Version: version.record.Version, SourceURL: version.record.SourceURL}
	}
	return nil
}

func (i *Installer) maintainNative(ctx context.Context, id string, owner nativeOwner, notify func(Progress)) (Result, error) {
	current := i.nativeOwnership(id, owner.Path, nil)
	if current == nil || current.Digest != owner.Digest {
		return Result{}, failure("native program ownership changed", nil)
	}
	return i.prepareProgram(ctx, id, notify, false, func(staging string, record installRecord) (Result, error) {
		if ctx.Err() != nil {
			return Result{}, canceled(ctx)
		}
		if record.Node || len(record.Args) != 0 {
			return Result{}, failure("native replacement needs a complete bundle", nil)
		}
		candidate, err := confinedFile(filepath.Join(staging, "payload"), record.Binary)
		if err != nil {
			return Result{}, err
		}
		if !nativeImage(candidate) {
			return Result{}, failure("native replacement executable format", nil)
		}
		info, err := os.Lstat(owner.Path)
		if err != nil || !info.Mode().IsRegular() {
			return Result{}, failure("native target ownership", err)
		}
		next, err := os.CreateTemp(filepath.Dir(owner.Path), ".neuxbot-update-")
		if err != nil {
			return Result{}, failure("create native replacement", err)
		}
		nextPath := next.Name()
		defer os.Remove(nextPath)
		input, err := os.Open(candidate)
		if err != nil {
			next.Close()
			return Result{}, failure("read native replacement", err)
		}
		_, copyErr := io.Copy(next, &contextReader{ctx: ctx, reader: input})
		input.Close()
		if err := errors.Join(copyErr, next.Sync(), next.Close(), os.Chmod(nextPath, info.Mode().Perm())); err != nil {
			return Result{}, failure("stage native replacement", err)
		}
		if ctx.Err() != nil {
			return Result{}, canceled(ctx)
		}
		if digest, err := digestFile(owner.Path); err != nil || digest != owner.Digest {
			return Result{}, failure("native target changed before replacement", err)
		}
		receipts := filepath.Join(i.root, ".external-receipts")
		if err := os.Mkdir(receipts, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return Result{}, failure("create native ownership directory", err)
		}
		if info, err := os.Lstat(receipts); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return Result{}, failure("native ownership directory", err)
		}
		receipt := nativeOwner{RuntimeID: id, Path: owner.Path, Digest: record.Digests[record.Binary], Version: record.Version, SourceURL: record.SourceURL}
		data, err := json.Marshal(receipt)
		if err != nil {
			return Result{}, failure("encode native ownership", err)
		}
		metadata, err := os.CreateTemp(receipts, ".native-next-")
		if err != nil {
			return Result{}, failure("create native ownership record", err)
		}
		metadataPath := metadata.Name()
		defer os.Remove(metadataPath)
		_, writeErr := metadata.Write(data)
		if err := errors.Join(writeErr, metadata.Sync(), metadata.Close()); err != nil {
			return Result{}, failure("stage native ownership record", err)
		}
		backup, err := os.CreateTemp(filepath.Dir(owner.Path), ".neuxbot-previous-")
		if err != nil {
			return Result{}, failure("create native rollback", err)
		}
		backupPath := backup.Name()
		backup.Close()
		os.Remove(backupPath)
		if err := os.Link(owner.Path, backupPath); err != nil {
			return Result{}, failure("retain native rollback", err)
		}
		retainBackup := false
		defer func() {
			if !retainBackup {
				_ = os.Remove(backupPath)
			}
		}()
		if ctx.Err() != nil {
			return Result{}, canceled(ctx)
		}
		if err := os.Rename(nextPath, owner.Path); err != nil {
			return Result{}, failure("replace selected native program", err)
		}
		if err := os.Rename(metadataPath, i.nativeReceiptPath(id, owner.Path)); err != nil {
			if rollbackErr := os.Rename(backupPath, owner.Path); rollbackErr != nil {
				retainBackup = true
				return Result{}, failure("restore selected native program", rollbackErr)
			}
			return Result{}, failure("publish native ownership record", err)
		}
		return Result{Binary: owner.Path, Version: record.Version, SourceURL: record.SourceURL}, nil
	})
}

func (i *Installer) uninstallNative(ctx context.Context, id string, owner nativeOwner) error {
	current := i.nativeOwnership(id, owner.Path, nil)
	if current == nil || current.Digest != owner.Digest {
		return failure("native uninstall ownership changed", nil)
	}
	if ctx.Err() != nil {
		return canceled(ctx)
	}
	if err := os.Remove(owner.Path); err != nil {
		return failure("remove selected native program", err)
	}
	if err := os.Remove(i.nativeReceiptPath(id, owner.Path)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return failure("remove native ownership record", err)
	}
	return nil
}
