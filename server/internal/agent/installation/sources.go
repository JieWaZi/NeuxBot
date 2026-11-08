// Package installation installs official CLI artifacts into an isolated, versioned program store.
package installation

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// Source describes the publisher and the verification available for an installation.
type Source struct {
	// RuntimeID identifies the built-in CLI, independently of profiles.
	RuntimeID string
	// URL is the official installation or source documentation.
	URL string
	// Integrity names the publisher's digest, or HTTPS where no digest is published.
	Integrity string
	// RequiresNode indicates the external Node.js 20+ and npm prerequisites.
	RequiresNode bool
}

type artifact struct {
	// version is the publisher's immutable release identity.
	version string
	// url is the exact official artifact location.
	url string
	// fallbackURL is the same GitHub asset's official API when its browser route is unavailable.
	fallbackURL string
	// algorithm names the digest supplied by the publisher.
	algorithm string
	// digest is the expected digest encoded as lowercase hexadecimal.
	digest string
	// format selects a bare executable, gzip tar, or zip archive.
	format string
	// binary locates the executable relative to the extracted artifact root.
	binary string
	// args names additional managed files passed to an included interpreter.
	args []string
	// npmPackage is the official npm package requiring dependency installation.
	npmPackage string
}

var versionPattern = regexp.MustCompile(`^[0-9][0-9A-Za-z.+_-]{0,100}$`)
var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var githubAssetIDPattern = regexp.MustCompile(`^[0-9]+$`)
var cursorBuildPattern = regexp.MustCompile(`https://downloads\.cursor\.com/lab/([0-9][0-9A-Za-z.-]{0,100})/`)

var sources = map[string]Source{
	constants.RuntimeIDCodex:     {RuntimeID: constants.RuntimeIDCodex, URL: "https://github.com/openai/codex", Integrity: "publisher-sha256"},
	constants.RuntimeIDClaude:    {RuntimeID: constants.RuntimeIDClaude, URL: "https://code.claude.com/docs/en/setup", Integrity: "npm-sha512"},
	constants.RuntimeIDCursor:    {RuntimeID: constants.RuntimeIDCursor, URL: "https://cursor.com/docs/cli/installation", Integrity: "https"},
	constants.RuntimeIDKimi:      {RuntimeID: constants.RuntimeIDKimi, URL: "https://code.kimi.com/kimi-code/install.sh", Integrity: "publisher-sha256"},
	constants.RuntimeIDOpenCode:  {RuntimeID: constants.RuntimeIDOpenCode, URL: "https://opencode.ai/v2/docs/", Integrity: "npm-sha512"},
	constants.RuntimeIDPi:        {RuntimeID: constants.RuntimeIDPi, URL: "https://github.com/earendil-works/pi", Integrity: "publisher-sha256"},
	constants.RuntimeIDGrok:      {RuntimeID: constants.RuntimeIDGrok, URL: "https://grok.com/build", Integrity: "https"},
	constants.RuntimeIDGemini:    {RuntimeID: constants.RuntimeIDGemini, URL: "https://geminicli.com/docs/get-started/installation/", Integrity: "npm-sha512", RequiresNode: true},
	constants.RuntimeIDQoder:     {RuntimeID: constants.RuntimeIDQoder, URL: "https://docs.qoder.com/cli/installation", Integrity: "publisher-sha256"},
	constants.RuntimeIDQoderCN:   {RuntimeID: constants.RuntimeIDQoderCN, URL: "https://docs.qoder.cn/cli/installation", Integrity: "publisher-sha256"},
	constants.RuntimeIDWorkBuddy: {RuntimeID: constants.RuntimeIDWorkBuddy, URL: "https://www.workbuddy.cn/docs/cli/installation", Integrity: "publisher-sha256"},
}

// Source returns the publisher information for a supported CLI on this installer's platform.
func (i *Installer) Source(runtimeID string) (Source, bool) {
	source, exists := sources[runtimeID]
	// The reviewed Linux artifacts require glibc, except Codex's explicitly musl-linked package.
	if i.os == "linux" && runtimeID != constants.RuntimeIDCodex {
		for _, loader := range []string{"/lib/ld-musl-x86_64.so.1", "/lib/ld-musl-aarch64.so.1", "/lib/libc.musl-x86_64.so.1", "/lib/libc.musl-aarch64.so.1"} {
			if _, err := os.Stat(loader); err == nil {
				return source, false
			}
		}
	}
	return source, exists && (i.os == "darwin" || i.os == "linux" || i.os == "windows") &&
		(i.arch == "amd64" || i.arch == "arm64") && !(i.os == "windows" && i.arch != "amd64")
}

// Supported reports a known installation mechanism, not whether external prerequisites are present.
func (i *Installer) Supported(runtimeID string) bool { _, ok := i.Source(runtimeID); return ok }

func (i *Installer) resolve(ctx context.Context, runtimeID string) (artifact, error) {
	if !i.Supported(runtimeID) {
		return artifact{}, status_error.RuntimeInstallationUnsupported
	}
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[i.arch]
	target := i.os + "-" + arch
	ext := ""
	if i.os == "windows" {
		ext = ".exe"
	}
	switch runtimeID {
	case constants.RuntimeIDCodex:
		triple := map[string]string{"darwin-x64": "x86_64-apple-darwin", "darwin-arm64": "aarch64-apple-darwin", "linux-x64": "x86_64-unknown-linux-musl", "linux-arm64": "aarch64-unknown-linux-musl", "windows-x64": "x86_64-pc-windows-msvc"}[target]
		return i.github(ctx, "openai/codex", "rust-v", func(string) string { return "codex-package-" + triple + ".tar.gz" }, "bin/codex"+ext)
	case constants.RuntimeIDKimi:
		// Python kimi-cli 1.52.0 is a migration stub, including for `acp`.
		// Kimi's official installer declares this self-contained 2.x manifest.
		base := "https://code.kimi.com/kimi-code"
		data, err := i.read(ctx, base+"/latest", metadataLimit)
		if err != nil {
			return artifact{}, err
		}
		v := strings.TrimSpace(string(data))
		if !versionPattern.MatchString(v) {
			return artifact{}, failure("kimi release version", nil)
		}
		manifest, err := i.json(ctx, base+"/binaries/"+url.PathEscape(v)+"/manifest.json")
		if err != nil {
			return artifact{}, err
		}
		platform := target
		if i.os == "windows" {
			platform = "win32-" + arch
		}
		row := objectValue(objectValue(manifest["platforms"])[platform])
		filename, checksum := textValue(row["filename"]), textValue(row["checksum"])
		if textValue(manifest["version"]) != v || filename != "kimi-code-"+platform+ext || !sha256Pattern.MatchString(checksum) {
			return artifact{}, failure("kimi release manifest", nil)
		}
		return artifact{version: v, url: base + "/binaries/" + url.PathEscape(v) + "/" + filename, algorithm: "sha256", digest: checksum, format: "binary", binary: "kimi" + ext}, nil
	case constants.RuntimeIDPi:
		platform := target
		if i.os == "windows" {
			platform = "windows-x64"
		}
		binary := "pi/pi"
		if i.os == "windows" {
			binary = "pi.exe"
		}
		return i.github(ctx, "earendil-works/pi", "v", func(string) string { return "pi-" + platform + "." + archiveExtension(i.os) }, binary)
	case constants.RuntimeIDClaude:
		manifest, err := i.json(ctx, "https://registry.npmjs.org/@anthropic-ai/claude-agent-sdk/latest")
		if err != nil {
			return artifact{}, err
		}
		v, cli := textValue(manifest["version"]), textValue(manifest["claudeCodeVersion"])
		if !versionPattern.MatchString(v) || !versionPattern.MatchString(cli) {
			return artifact{}, failure("claude release manifest", nil)
		}
		platform := target
		if i.os == "windows" {
			platform = "win32-" + arch
		}
		result, err := i.npmArtifact(ctx, "@anthropic-ai/claude-agent-sdk-"+platform, v, "package/claude"+ext)
		result.version = cli
		return result, err
	case constants.RuntimeIDOpenCode:
		manifest, err := i.json(ctx, "https://registry.npmjs.org/@opencode/cli/latest")
		if err != nil {
			return artifact{}, err
		}
		return i.npmArtifact(ctx, "@opencode/cli-"+target, textValue(manifest["version"]), "package/bin/opencode"+ext)
	case constants.RuntimeIDGemini:
		result, err := i.npmArtifact(ctx, "@google/gemini-cli", "latest", "")
		result.npmPackage = "@google/gemini-cli"
		return result, err
	case constants.RuntimeIDCursor:
		data, err := i.read(ctx, "https://cursor.com/install", metadataLimit)
		if err != nil {
			return artifact{}, err
		}
		match := cursorBuildPattern.FindSubmatch(data)
		if len(match) != 2 {
			return artifact{}, failure("cursor installer release manifest", nil)
		}
		platform := i.os
		if platform == "windows" {
			platform = "windows"
		}
		result := artifact{version: string(match[1]), url: "https://downloads.cursor.com/lab/" + string(match[1]) + "/" + platform + "/" + arch + "/agent-cli-package." + archiveExtension(i.os), format: archiveExtension(i.os), binary: "dist-package/cursor-agent"}
		if i.os == "windows" {
			result.binary, result.args = "dist-package/node.exe", []string{"dist-package/index.js"}
		}
		return result, nil
	case constants.RuntimeIDGrok:
		data, err := i.read(ctx, "https://x.ai/cli/stable", metadataLimit)
		if err != nil {
			return artifact{}, err
		}
		v := strings.TrimSpace(string(data))
		if !versionPattern.MatchString(v) {
			return artifact{}, failure("grok stable version", nil)
		}
		platform := map[string]string{"darwin": "macos", "linux": "linux", "windows": "windows"}[i.os]
		cpu := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[i.arch]
		return artifact{version: v, url: "https://x.ai/cli/grok-" + v + "-" + platform + "-" + cpu + ext, format: "binary", binary: "grok" + ext}, nil
	case constants.RuntimeIDQoder, constants.RuntimeIDQoderCN:
		base, binary := "https://qoder-ide.oss-accelerate.aliyuncs.com/qodercli", "qodercli"
		if runtimeID == constants.RuntimeIDQoderCN {
			base, binary = "https://static.qoder.com.cn/qoder-cli-cn", "qoderclicn"
		}
		manifest, err := i.json(ctx, base+"/channels/manifest.json")
		if err != nil {
			return artifact{}, err
		}
		v := textValue(manifest["latest"])
		if !versionPattern.MatchString(v) {
			return artifact{}, failure("qoder release version", nil)
		}
		cpu := i.arch
		// The baseline x64 build avoids requiring AVX2 on unknown Linux CPUs.
		if i.os == "linux" && cpu == "amd64" {
			cpu += "-baseline"
		}
		for _, raw := range arrayValue(manifest["files"]) {
			row := objectValue(raw)
			if textValue(row["os"]) != i.os || textValue(row["arch"]) != cpu || textValue(row["variant"]) == "legacy" {
				continue
			}
			u, digest := textValue(row["url"]), textValue(row["sha256"])
			if !strings.HasPrefix(u, base+"/releases/"+v+"/") || !sha256Pattern.MatchString(digest) {
				return artifact{}, failure("qoder release artifact", nil)
			}
			return artifact{version: v, url: u, algorithm: "sha256", digest: digest, format: formatOf(u), binary: binary + ext}, nil
		}
		return artifact{}, status_error.RuntimeInstallationUnsupported
	case constants.RuntimeIDWorkBuddy:
		const base = "https://acc-1258344699.cos.accelerate.myqcloud.com/@tencent-ai/codebuddy-code/releases"
		data, err := i.read(ctx, base+"/latest", metadataLimit)
		if err != nil {
			return artifact{}, err
		}
		v := strings.TrimSpace(string(data))
		if !versionPattern.MatchString(v) {
			return artifact{}, failure("workbuddy release version", nil)
		}
		platform := map[string]string{"darwin": "Darwin", "linux": "Linux", "windows": "Windows"}[i.os]
		cpu := map[string]string{"amd64": "x86_64", "arm64": "arm64"}[i.arch]
		name := "codebuddy-code_" + platform + "_" + cpu + "." + archiveExtension(i.os)
		checksums, err := i.read(ctx, base+"/download/"+v+"/checksums.txt", metadataLimit)
		if err != nil {
			return artifact{}, err
		}
		for _, line := range strings.Split(string(checksums), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name && sha256Pattern.MatchString(fields[0]) {
				return artifact{version: v, url: base + "/download/" + v + "/" + name, algorithm: "sha256", digest: fields[0], format: archiveExtension(i.os), binary: "codebuddy" + ext}, nil
			}
		}
	}
	return artifact{}, failure("publisher artifact missing", nil)
}

func (i *Installer) github(ctx context.Context, repository, prefix string, assetName func(string) string, binary string) (artifact, error) {
	doc, err := i.json(ctx, "https://api.github.com/repos/"+repository+"/releases/latest")
	if err != nil {
		return artifact{}, err
	}
	tag := textValue(doc["tag_name"])
	v := strings.TrimPrefix(tag, prefix)
	if !strings.HasPrefix(tag, prefix) || !versionPattern.MatchString(v) {
		return artifact{}, failure("github release version", nil)
	}
	name := assetName(v)
	for _, value := range arrayValue(doc["assets"]) {
		row := objectValue(value)
		digest := strings.TrimPrefix(textValue(row["digest"]), "sha256:")
		if textValue(row["name"]) == name && sha256Pattern.MatchString(digest) {
			assetAPI := textValue(row["url"])
			prefix := "https://api.github.com/repos/" + repository + "/releases/assets/"
			if !strings.HasPrefix(assetAPI, prefix) || !githubAssetIDPattern.MatchString(strings.TrimPrefix(assetAPI, prefix)) {
				assetAPI = ""
			}
			return artifact{version: v, url: "https://github.com/" + repository + "/releases/download/" + url.PathEscape(tag) + "/" + name, fallbackURL: assetAPI, algorithm: "sha256", digest: digest, format: formatOf(name), binary: binary}, nil
		}
	}
	return artifact{}, failure("github release digest missing", nil)
}

func (i *Installer) npmArtifact(ctx context.Context, name, version, binary string) (artifact, error) {
	if version != "latest" && !versionPattern.MatchString(version) {
		return artifact{}, failure("npm release version", nil)
	}
	doc, err := i.json(ctx, "https://registry.npmjs.org/"+name+"/"+version)
	if err != nil {
		return artifact{}, err
	}
	dist := objectValue(doc["dist"])
	v, location, integrity := textValue(doc["version"]), textValue(dist["tarball"]), textValue(dist["integrity"])
	if textValue(doc["name"]) != name || !versionPattern.MatchString(v) || (version != "latest" && v != version) ||
		!strings.HasPrefix(location, "https://registry.npmjs.org/"+name+"/-/") || !strings.HasPrefix(integrity, "sha512-") {
		return artifact{}, failure("npm artifact identity", nil)
	}
	digest, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(integrity, "sha512-"))
	if err != nil || len(digest) != 64 {
		return artifact{}, failure("npm artifact integrity", err)
	}
	if binary == "" {
		binary = textValue(objectValue(doc["bin"])["gemini"])
		if binary == "" || !safeRelative(binary) {
			return artifact{}, failure("npm executable entry", nil)
		}
	}
	return artifact{version: v, url: location, algorithm: "sha512", digest: hex.EncodeToString(digest), format: "tar.gz", binary: binary}, nil
}

func (i *Installer) json(ctx context.Context, location string) (map[string]any, error) {
	data, err := i.read(ctx, location, metadataLimit)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, failure("decode publisher manifest", err)
	}
	return value, nil
}

func textValue(value any) string           { result, _ := value.(string); return result }
func objectValue(value any) map[string]any { result, _ := value.(map[string]any); return result }
func arrayValue(value any) []any           { result, _ := value.([]any); return result }
func archiveExtension(goos string) string {
	if goos == "windows" {
		return "zip"
	}
	return "tar.gz"
}
func formatOf(location string) string {
	if strings.HasSuffix(path.Base(location), ".zip") {
		return "zip"
	}
	return "tar.gz"
}
