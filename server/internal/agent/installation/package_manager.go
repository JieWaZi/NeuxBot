package installation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type packageOwner struct {
	// method is npm or homebrew, established from the selected entry and local receipts.
	method string
	// executable is the matching manager executable, not the selected program's shim.
	executable string
	// prefix is the proven global npm or Homebrew installation root.
	prefix string
	// name is an allowlisted official package or installed formula/cask token.
	name string
	// cask distinguishes Homebrew casks from formulas.
	cask bool
	// identity is the local package manifest or Homebrew installation receipt.
	identity string
	// digest detects an ownership change between inspection and mutation.
	digest string
	// binary retains the exact original selected entry for post-update validation.
	binary string
	// args retains the original interpreter prefix for script-based selections.
	args []string
	// command identifies the declared npm bin, independently of Windows shim extensions.
	command string
}

var npmPackages = map[string][]string{
	"codex": {"@openai/codex"}, "claude": {"@anthropic-ai/claude-code"},
	"kimi": {"@moonshot-ai/kimi-code"}, "opencode": {"@opencode/cli", "opencode-ai"},
	"pi":     {"@earendil-works/pi-coding-agent", "@mariozechner/pi-coding-agent"},
	"gemini": {"@google/gemini-cli"}, "qoder": {"@qoder-ai/qodercli"},
	"workbuddy": {"@tencent-ai/codebuddy-code"},
}

var brewPackages = map[string]string{"codex": "codex", "claude": "claude-code", "gemini": "gemini-cli", "opencode": "opencode"}

func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func readLocalJSON(name string) (map[string]any, bool) {
	info, err := os.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > metadataLimit {
		return nil, false
	}
	data, err := os.ReadFile(name)
	var result map[string]any
	if err != nil || json.Unmarshal(data, &result) != nil {
		return nil, false
	}
	return result, true
}

func resolvedFile(name string) string {
	result, err := filepath.EvalSymlinks(name)
	if err != nil {
		return ""
	}
	return result
}

func (i *Installer) packageOwnership(id, binary string, args []string) *packageOwner {
	selected := binary
	if len(args) > 0 && filepath.IsAbs(args[0]) {
		selected = args[0]
	}
	real := resolvedFile(selected)
	if real == "" {
		return nil
	}
	if owner := i.brewOwnership(id, real); owner != nil {
		owner.binary, owner.args = binary, append([]string(nil), args...)
		return owner
	}
	// A failed Homebrew receipt check must not fall through to its embedded npm tree.
	if strings.Contains(filepath.ToSlash(real), "/Cellar/") || strings.Contains(filepath.ToSlash(real), "/Caskroom/") {
		return nil
	}
	for parent := filepath.Dir(real); parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
		manifest := filepath.Join(parent, "package.json")
		doc, ok := readLocalJSON(manifest)
		if !ok {
			continue
		}
		name := textValue(doc["name"])
		if !containsString(npmPackages[id], name) || !packageBinMatches(parent, doc, real) {
			continue
		}
		packageRelative := filepath.Join(strings.Split(name, "/")...)
		moduleRoot := strings.TrimSuffix(parent, string(filepath.Separator)+packageRelative)
		if moduleRoot == parent || filepath.Base(moduleRoot) != "node_modules" {
			continue
		}
		prefix := filepath.Dir(moduleRoot)
		if i.os != "windows" {
			if filepath.Base(prefix) != "lib" {
				continue
			}
			prefix = filepath.Dir(prefix)
			command := packageBinName(parent, doc, real)
			if command == "" || !samePath(resolvedFile(filepath.Join(prefix, "bin", command)), real) {
				continue
			}
		} else if !samePath(filepath.Dir(binary), prefix) && len(args) == 0 {
			continue
		}
		manager, _ := i.lookPath("npm")
		digest, err := digestFile(manifest)
		if err != nil {
			return nil
		}
		return &packageOwner{method: "npm", executable: manager, prefix: prefix, name: name, identity: manifest, digest: digest, binary: binary, args: append([]string(nil), args...), command: packageBinName(parent, doc, real)}
	}
	if i.os == "windows" {
		// npm's cmd/PowerShell launchers are files, not executable payloads. Bind the
		// shim to the adjacent global manifest and its exact declared bin path.
		prefix := filepath.Dir(binary)
		info, err := os.Lstat(binary)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<10 {
			return nil
		}
		data, err := os.ReadFile(binary)
		if err != nil {
			return nil
		}
		for _, name := range npmPackages[id] {
			root := filepath.Join(prefix, "node_modules", filepath.FromSlash(name))
			manifest := filepath.Join(root, "package.json")
			doc, ok := readLocalJSON(manifest)
			if !ok || textValue(doc["name"]) != name {
				continue
			}
			for _, relative := range packageBins(doc) {
				if !safeRelative(strings.TrimPrefix(relative, "./")) {
					continue
				}
				marker := "node_modules/" + name + "/" + strings.TrimPrefix(relative, "./")
				if !strings.Contains(strings.ReplaceAll(string(data), `\`, "/"), marker) {
					continue
				}
				if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); err != nil {
					continue
				}
				manager, _ := i.lookPath("npm")
				digest, err := digestFile(manifest)
				if err != nil {
					return nil
				}
				return &packageOwner{method: "npm", executable: manager, prefix: prefix, name: name, identity: manifest, digest: digest, binary: binary, args: append([]string(nil), args...), command: packageBinName(root, doc, resolvedFile(filepath.Join(root, filepath.FromSlash(relative))))}
			}
		}
	}
	return nil
}

func packageBins(doc map[string]any) []string {
	if single := textValue(doc["bin"]); single != "" {
		return []string{single}
	}
	var paths []string
	for _, value := range objectValue(doc["bin"]) {
		if text := textValue(value); text != "" {
			paths = append(paths, text)
		}
	}
	return paths
}

func packageBinMatches(root string, doc map[string]any, entry string) bool {
	for _, relative := range packageBins(doc) {
		relative = strings.TrimPrefix(relative, "./")
		if safeRelative(relative) && samePath(resolvedFile(filepath.Join(root, filepath.FromSlash(relative))), entry) {
			return true
		}
	}
	return false
}

func packageBinName(root string, doc map[string]any, entry string) string {
	if textValue(doc["bin"]) != "" {
		return filepath.Base(textValue(doc["name"]))
	}
	for name, value := range objectValue(doc["bin"]) {
		if samePath(resolvedFile(filepath.Join(root, filepath.FromSlash(textValue(value)))), entry) {
			return name
		}
	}
	return ""
}

func (i *Installer) packageProbe(owner packageOwner) (string, []string, error) {
	if owner.method == "homebrew" {
		entry := map[string]string{"codex": "codex", "claude-code": "claude", "gemini-cli": "gemini", "opencode": "opencode"}[owner.name]
		candidate := filepath.Join(owner.prefix, "bin", entry)
		if verified := i.brewOwnership(map[string]string{"codex": "codex", "claude-code": "claude", "gemini-cli": "gemini", "opencode": "opencode"}[owner.name], resolvedFile(candidate)); verified != nil && verified.prefix == owner.prefix && verified.name == owner.name {
			return candidate, nil, nil
		}
		return "", nil, failure("updated Homebrew entry ownership", nil)
	}
	doc, ok := readLocalJSON(owner.identity)
	if !ok || textValue(doc["name"]) != owner.name {
		return "", nil, failure("updated npm package ownership", nil)
	}
	relative := textValue(doc["bin"])
	if relative == "" {
		relative = textValue(objectValue(doc["bin"])[owner.command])
	}
	relative = strings.TrimPrefix(relative, "./")
	if !safeRelative(relative) {
		return "", nil, failure("updated npm bin ownership", nil)
	}
	entry, err := confinedFile(filepath.Dir(owner.identity), relative)
	if err != nil {
		return "", nil, err
	}
	if i.os != "windows" {
		return entry, nil, nil
	}
	file, err := os.Open(entry)
	if err != nil {
		return "", nil, failure("read npm executable format", err)
	}
	var header [4]byte
	_, readErr := file.Read(header[:])
	file.Close()
	if readErr == nil && header[0] == 'M' && header[1] == 'Z' {
		return entry, nil, nil
	}
	if strings.HasSuffix(entry, ".js") || strings.HasSuffix(entry, ".mjs") || strings.HasSuffix(entry, ".cjs") {
		node, err := i.lookPath("node")
		if err != nil {
			return "", nil, failure("Node.js prerequisite missing", err)
		}
		return node, []string{entry}, nil
	}
	return "", nil, failure("npm Windows launcher unsupported", nil)
}

func (i *Installer) brewOwnership(id, real string) *packageOwner {
	wanted := brewPackages[id]
	if wanted == "" {
		return nil
	}
	for root := filepath.Dir(real); root != filepath.Dir(root); root = filepath.Dir(root) {
		versionRoot := root
		tokenRoot := filepath.Dir(versionRoot)
		container := filepath.Dir(tokenRoot)
		kind := filepath.Base(container)
		if (kind != "Cellar" && kind != "Caskroom") || filepath.Base(tokenRoot) != wanted {
			continue
		}
		prefix := filepath.Dir(container)
		manager := filepath.Join(prefix, "bin", "brew")
		if _, err := os.Stat(manager); err != nil {
			manager = ""
		}
		if kind == "Cellar" {
			manifest := filepath.Join(versionRoot, "INSTALL_RECEIPT.json")
			doc, ok := readLocalJSON(manifest)
			if !ok || textValue(objectValue(doc["source"])["tap"]) != "homebrew/core" {
				return nil
			}
			digest, err := digestFile(manifest)
			if err != nil {
				return nil
			}
			return &packageOwner{method: "homebrew", executable: manager, prefix: prefix, name: wanted, identity: manifest, digest: digest}
		}
		metadata := filepath.Join(tokenRoot, ".metadata", filepath.Base(versionRoot))
		entries, err := os.ReadDir(metadata)
		if err != nil {
			return nil
		}
		for _, timestamp := range entries {
			manifest := filepath.Join(metadata, timestamp.Name(), "Casks", wanted+".json")
			doc, ok := readLocalJSON(manifest)
			if !ok || textValue(doc["token"]) != wanted {
				continue
			}
			digest, err := digestFile(manifest)
			if err != nil {
				return nil
			}
			return &packageOwner{method: "homebrew", executable: manager, prefix: prefix, name: wanted, cask: true, identity: manifest, digest: digest}
		}
	}
	return nil
}

func (i *Installer) maintainPackage(ctx context.Context, id string, owner packageOwner, uninstall bool, notify func(Progress)) (Result, error) {
	current := i.packageOwnership(id, owner.binary, owner.args)
	if current == nil || current.method != owner.method || current.prefix != owner.prefix || current.name != owner.name || current.digest != owner.digest || current.executable != owner.executable {
		return Result{}, failure("package ownership changed", nil)
	}
	if owner.executable == "" {
		return Result{}, failure("package manager prerequisite missing", nil)
	}
	if err := os.MkdirAll(i.root, 0o700); err != nil {
		return Result{}, failure("create package maintenance scratch", err)
	}
	scratch, err := os.MkdirTemp(i.root, ".package-maintenance-")
	if err != nil {
		return Result{}, failure("create package maintenance scratch", err)
	}
	defer os.RemoveAll(scratch)
	for _, name := range []string{"home", "cache", "tmp"} {
		if err := os.Mkdir(filepath.Join(scratch, name), 0o700); err != nil {
			return Result{}, failure("create package maintenance scratch", err)
		}
	}
	env := privateEnvironment(scratch)
	var args []string
	if owner.method == "npm" {
		verb, target := "install", owner.name+"@latest"
		if uninstall {
			verb, target = "uninstall", owner.name
		}
		args = []string{verb, "--global", "--prefix", owner.prefix, "--no-audit", "--no-fund", "--registry=https://registry.npmjs.org", target}
		if uninstall {
			args = append(args, "--ignore-scripts")
		}
	} else {
		verb := "upgrade"
		if uninstall {
			verb = "uninstall"
		}
		kind := "--formula"
		if owner.cask {
			kind = "--cask"
		}
		args = []string{verb, kind, owner.name}
		env = append(env, "HOMEBREW_NO_INSTALL_CLEANUP=1", "HOMEBREW_NO_ANALYTICS=1", "HOMEBREW_NO_ENV_HINTS=1")
	}
	executable := owner.executable
	if i.os == "windows" && owner.method == "npm" {
		node, err := i.lookPath("node")
		if err != nil {
			return Result{}, failure("Node.js prerequisite missing", err)
		}
		entry := filepath.Join(filepath.Dir(owner.executable), "node_modules", "npm", "bin", "npm-cli.js")
		if _, err := os.Stat(entry); err != nil {
			return Result{}, failure("npm Node entry unavailable", err)
		}
		executable, args = node, append([]string{entry}, args...)
	}
	if notify != nil {
		notify(Progress{Phase: "dependencies"})
	}
	if _, err := i.runner.Run(ctx, Command{Binary: executable, Args: args, Dir: scratch, Env: env}); err != nil {
		if ctx.Err() != nil {
			return Result{}, canceled(ctx)
		}
		return Result{}, failure("package manager maintenance command", err)
	}
	if uninstall {
		if _, err := os.Lstat(owner.identity); !errors.Is(err, os.ErrNotExist) {
			return Result{}, failure("package remains after uninstall", err)
		}
		return Result{}, nil
	}
	if notify != nil {
		notify(Progress{Phase: "verifying"})
	}
	probe, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	probeBinary, probeArgs, err := i.packageProbe(owner)
	if err != nil {
		return Result{}, err
	}
	output, err := i.runner.Run(probe, Command{Binary: probeBinary, Args: append(append([]string(nil), probeArgs...), "--version"), Dir: scratch, Env: env})
	if ctx.Err() != nil {
		return Result{}, canceled(ctx)
	}
	if err != nil {
		return Result{}, failure("verify updated package version", err)
	}
	version := versionToken(output)
	if version == "" {
		return Result{}, failure("updated package version unreadable", nil)
	}
	return Result{Binary: probeBinary, PrefixArgs: probeArgs, Version: strings.TrimPrefix(strings.TrimPrefix(version, "rust-"), "v"), SourceURL: sources[id].URL}, nil
}
