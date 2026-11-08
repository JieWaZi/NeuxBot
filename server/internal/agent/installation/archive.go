package installation

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const expandedLimit int64 = 2 << 30
const entryLimit = 150000

type archiveLink struct {
	// name is the destination relative to the extraction root.
	name string
	// target is a validated internal link target.
	target string
	// hard distinguishes tar hard links from symbolic links.
	hard bool
}

func safeRelative(name string) bool {
	if name == "" || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

// confinedFile checks resolved paths as well as lexical containment, including internal symlinks.
func confinedFile(root, relative string) (string, error) {
	if !safeRelative(relative) {
		return "", failure("unsafe installed path", nil)
	}
	name := filepath.Join(root, filepath.FromSlash(relative))
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", failure("resolve program directory", err)
	}
	real, err := filepath.EvalSymlinks(name)
	if err != nil {
		return "", failure("resolve executable", err)
	}
	inside, err := filepath.Rel(realRoot, real)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) || filepath.IsAbs(inside) {
		return "", failure("executable escapes program directory", err)
	}
	info, err := os.Stat(real)
	if err != nil || !info.Mode().IsRegular() {
		return "", failure("executable is not a regular file", err)
	}
	return name, nil
}

func extract(ctx context.Context, source, root, format, executable string) error {
	if format == "binary" {
		file, err := os.Open(source)
		if err != nil {
			return failure("open downloaded binary", err)
		}
		defer file.Close()
		return writeEntry(ctx, root, executable, 0o755, file, downloadLimit)
	}
	var links []archiveLink
	var expanded int64
	var count int
	add := func(name string, size int64) (string, error) {
		name = strings.TrimSuffix(strings.TrimPrefix(name, "./"), "/")
		if !safeRelative(name) || size < 0 || size > expandedLimit || expanded > expandedLimit-size || count >= entryLimit {
			return "", failure("unsafe archive entry or expansion limit", nil)
		}
		expanded += size
		count++
		return name, nil
	}
	switch format {
	case "tar.gz":
		file, err := os.Open(source)
		if err != nil {
			return failure("open downloaded archive", err)
		}
		defer file.Close()
		compressed, err := gzip.NewReader(file)
		if err != nil {
			return failure("decode gzip artifact", err)
		}
		defer compressed.Close()
		reader := tar.NewReader(compressed)
		for {
			if ctx.Err() != nil {
				return canceled(ctx)
			}
			header, err := reader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return failure("decode tar artifact", err)
			}
			if header.Name == "." || header.Name == "./" {
				continue
			}
			name, err := add(header.Name, header.Size)
			if err != nil {
				return err
			}
			switch header.Typeflag {
			case tar.TypeDir:
				if err := makeEntryParents(root, name+"/directory"); err != nil {
					return err
				}
			case tar.TypeReg, tar.TypeRegA:
				if err := writeEntry(ctx, root, name, os.FileMode(header.Mode), reader, header.Size); err != nil {
					return err
				}
			case tar.TypeSymlink, tar.TypeLink:
				links = append(links, archiveLink{name: name, target: header.Linkname, hard: header.Typeflag == tar.TypeLink})
			default:
				return failure("unsupported special archive file", nil)
			}
		}
		// Read the gzip trailer too; tar's end marker can precede its CRC and length check.
		trailing, err := io.Copy(io.Discard, io.LimitReader(compressed, expandedLimit-expanded+1))
		if err != nil || trailing > expandedLimit-expanded {
			return failure("gzip trailer or expansion limit", err)
		}
	case "zip":
		reader, err := zip.OpenReader(source)
		if err != nil {
			return failure("decode zip artifact", err)
		}
		defer reader.Close()
		for _, file := range reader.File {
			if ctx.Err() != nil {
				return canceled(ctx)
			}
			if file.UncompressedSize64 > uint64(expandedLimit) {
				return failure("zip expansion limit", nil)
			}
			name, err := add(file.Name, int64(file.UncompressedSize64))
			if err != nil {
				return err
			}
			if file.FileInfo().IsDir() {
				if err := makeEntryParents(root, name+"/directory"); err != nil {
					return err
				}
				continue
			}
			contents, err := file.Open()
			if err != nil {
				return failure("open zip entry", err)
			}
			if file.Mode()&os.ModeSymlink != 0 {
				target, readErr := io.ReadAll(io.LimitReader(contents, 4097))
				contents.Close()
				if readErr != nil || len(target) > 4096 {
					return failure("read archive link", readErr)
				}
				links = append(links, archiveLink{name: name, target: string(target)})
			} else {
				if !file.Mode().IsRegular() {
					contents.Close()
					return failure("unsupported special zip file", nil)
				}
				err := writeEntry(ctx, root, name, file.Mode(), contents, int64(file.UncompressedSize64))
				closeErr := contents.Close()
				if err != nil {
					return err
				}
				if closeErr != nil {
					return failure("close zip entry", closeErr)
				}
			}
		}
	default:
		return failure("unsupported artifact format", nil)
	}
	// Materialize links last. No regular file can subsequently traverse a link created by the archive.
	for _, hard := range []bool{true, false} {
		for _, link := range links {
			if link.hard != hard {
				continue
			}
			if ctx.Err() != nil {
				return canceled(ctx)
			}
			if strings.ContainsAny(link.target, "\\:\x00") || path.IsAbs(link.target) {
				return failure("absolute archive link", nil)
			}
			target := path.Clean(path.Join(path.Dir(link.name), link.target))
			if hard {
				target = path.Clean(link.target)
			}
			if !safeRelative(target) {
				return failure("archive link escapes payload", nil)
			}
			if err := makeEntryParents(root, link.name); err != nil {
				return err
			}
			destination := filepath.Join(root, filepath.FromSlash(link.name))
			if hard {
				source, err := confinedFile(root, target)
				if err != nil {
					return err
				}
				if err := os.Link(source, destination); err != nil {
					return failure("create internal archive hard link", err)
				}
			} else if err := os.Symlink(link.target, destination); err != nil {
				return failure("create internal archive symbolic link", err)
			}
		}
	}
	// Reject escaping link chains, including a link whose target was another later link.
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return failure("resolve extraction root", err)
	}
	for _, link := range links {
		real, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(link.name)))
		if err != nil {
			return failure("resolve extracted archive link", err)
		}
		relative, err := filepath.Rel(realRoot, real)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return failure("extracted link escapes payload", err)
		}
	}
	return nil
}

func makeEntryParents(root, name string) error {
	parent := path.Dir(name)
	if parent == "." {
		return nil
	}
	current := root
	for _, part := range strings.Split(parent, "/") {
		current = filepath.Join(current, part)
		if err := os.Mkdir(current, 0o700); err != nil && !os.IsExist(err) {
			return failure("create artifact directory", err)
		}
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return failure("archive parent is not a directory", err)
		}
	}
	return nil
}

func writeEntry(ctx context.Context, root, name string, mode os.FileMode, source io.Reader, size int64) error {
	if !safeRelative(name) {
		return failure("unsafe archive path", nil)
	}
	if err := makeEntryParents(root, name); err != nil {
		return err
	}
	permissions := os.FileMode(0o600)
	if mode&0o111 != 0 {
		permissions = 0o755
	}
	file, err := os.OpenFile(filepath.Join(root, filepath.FromSlash(name)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, permissions)
	if err != nil {
		return failure("create extracted artifact file", err)
	}
	defer file.Close()
	reader := &contextReader{ctx: ctx, reader: source}
	written, err := io.Copy(file, io.LimitReader(reader, size+1))
	if err != nil {
		if ctx.Err() != nil {
			return canceled(ctx)
		}
		return failure("extract artifact file", err)
	}
	if written > size {
		return failure("archive entry exceeds declared size", nil)
	}
	if err := file.Close(); err != nil {
		return failure("close extracted artifact file", err)
	}
	return nil
}

type contextReader struct {
	// ctx interrupts extraction between bounded reads.
	ctx context.Context
	// reader supplies the next archive entry's bytes.
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}
