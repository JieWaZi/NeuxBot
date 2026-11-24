package installation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// officialUpdate identifies an installed publisher CLI with a tested non-interactive updater.
type officialUpdate struct {
	// entry is the selected launch path, which the updater may relink.
	entry string
	// executable is the pinned vendor CLI used to run the updater.
	executable string
	// target is the canonical executable observed before maintenance.
	target string
	// home is the canonical user directory used to identify the publisher layout.
	home string
	// args is the fixed publisher upgrade command.
	args []string
}

func (i *Installer) officialUpdater(id, binary string, args []string) *officialUpdate {
	if i.os == "windows" || len(args) != 0 || !filepath.IsAbs(binary) {
		return nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	home, target := resolvedFile(userHome), resolvedFile(binary)
	if home == "" || target == "" {
		return nil
	}
	canonicalEntry := filepath.Join(resolvedFile(filepath.Dir(binary)), filepath.Base(binary))
	executable := binary
	var command []string
	switch id {
	case constants.RuntimeIDKimi:
		if samePath(target, filepath.Join(home, ".kimi-code", "bin", "kimi")) {
			command = []string{"upgrade", "--yes"}
		}
	case constants.RuntimeIDClaude:
		if samePath(canonicalEntry, filepath.Join(home, ".local", "bin", "claude")) &&
			samePath(filepath.Dir(target), filepath.Join(home, ".local", "share", "claude", "versions")) &&
			versionToken(filepath.Base(target)) != "" {
			command = []string{"update"}
		}
	case constants.RuntimeIDOpenCode:
		if samePath(target, filepath.Join(home, ".opencode", "bin", "opencode")) {
			command = []string{"upgrade", "--method", "curl"}
		}
	case constants.RuntimeIDGrok:
		if samePath(canonicalEntry, filepath.Join(home, ".grok", "bin", "grok")) &&
			(samePath(target, canonicalEntry) || inside(filepath.Join(home, ".grok", "downloads"), target)) {
			command = []string{"update"}
		}
	case constants.RuntimeIDCursor:
		binDir := filepath.Join(home, ".local", "bin")
		versions := filepath.Join(home, ".local", "share", "cursor-agent", "versions")
		versionDir := filepath.Dir(target)
		if samePath(filepath.Dir(canonicalEntry), binDir) &&
			(filepath.Base(canonicalEntry) == "agent" || filepath.Base(canonicalEntry) == "cursor-agent") &&
			samePath(filepath.Dir(versionDir), versions) && filepath.Base(target) == "cursor-agent" &&
			cursorVersionToken.MatchString(filepath.Base(versionDir)) {
			// The official launcher is a script that must stay paired with its Node bundle.
			_, nodeErr := confinedFile(versionDir, "node")
			_, scriptErr := confinedFile(versionDir, "index.js")
			if nodeErr == nil && scriptErr == nil {
				command = []string{"update"}
			}
		}
	case constants.RuntimeIDQoder:
		if samePath(target, filepath.Join(home, ".qoder", "entry", "qoder")) {
			// The vendor dispatcher forwards to the first qodercli on PATH. Bind
			// directly to that choice after verifying the publisher layout below.
			executable, err = i.lookPath("qodercli")
			if err != nil {
				return nil
			}
			canonicalEntry = filepath.Join(resolvedFile(filepath.Dir(executable)), filepath.Base(executable))
			target = resolvedFile(executable)
		}
		prefix := filepath.Join(home, ".qoder", "bin", "qodercli")
		version, versioned := strings.CutPrefix(filepath.Base(target), "qodercli-")
		if samePath(canonicalEntry, filepath.Join(home, ".local", "bin", "qodercli")) &&
			samePath(filepath.Dir(target), prefix) && versioned && versionToken(version) != "" {
			command = []string{"update"}
		}
	}
	if len(command) == 0 {
		return nil
	}
	info, err := os.Lstat(target)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 || id != constants.RuntimeIDCursor && !nativeImage(target) {
		return nil
	}
	return &officialUpdate{entry: binary, executable: executable, target: target, home: home, args: command}
}

func (i *Installer) updateOfficial(ctx context.Context, id string, owner officialUpdate, notify func(Progress)) (Result, error) {
	identity, err := os.Stat(owner.target)
	if err != nil {
		return Result{}, failure("inspect official update target", err)
	}
	// The vendor updater needs its own install/cache state under the real home.
	// Forward only common host settings, without account-specific environment overrides.
	env := []string{"HOME=" + owner.home, "CI=1", "NO_COLOR=1"}
	for _, name := range []string{"PATH", "HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "SSL_CERT_FILE", "SSL_CERT_DIR", "TMPDIR"} {
		if value := os.Getenv(name); value != "" {
			env = append(env, name+"="+value)
		}
	}
	version := func() (string, error) {
		probe, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		output, err := i.runner.Run(probe, Command{Binary: owner.executable, Args: []string{"--version"}, Dir: owner.home, Env: env})
		if ctx.Err() != nil {
			return "", canceled(ctx)
		}
		if probe.Err() != nil {
			return "", status_error.OperationTimeout
		}
		observed := versionToken(output)
		if err != nil || observed == "" {
			return "", failure("verify official CLI version", err)
		}
		return strings.TrimPrefix(strings.TrimPrefix(observed, "rust-"), "v"), nil
	}
	before, err := version()
	if err != nil {
		return Result{}, err
	}
	if id == constants.RuntimeIDKimi {
		if order, known := CompareVersions(id, before, "0.43.0"); !known || order < 0 {
			return Result{}, status_error.RuntimeMaintenanceUnsupported
		}
	}
	command := owner.args
	if id == constants.RuntimeIDClaude {
		// The native updater can follow the stable channel. Its floor must cover
		// both the installed version and the release offered for this operation.
		stable, err := i.read(ctx, "https://downloads.claude.ai/claude-code-releases/stable", metadataLimit)
		if err != nil {
			return Result{}, err
		}
		stableVersion := strings.TrimSpace(string(stable))
		if order, known := CompareVersions(id, before, stableVersion); !known || order > 0 {
			return Result{}, status_error.RuntimeMaintenanceUnsupported
		}
		release, err := i.CheckVersion(ctx, id)
		if err != nil {
			return Result{}, err
		}
		if order, known := CompareVersions(id, stableVersion, release.Version); !known || order < 0 {
			return Result{}, status_error.RuntimeMaintenanceUnsupported
		}
	}
	if id == constants.RuntimeIDOpenCode {
		release, err := i.CheckVersion(ctx, id)
		if err != nil {
			return Result{}, err
		}
		order, known := CompareVersions(id, before, release.Version)
		if !known || !SameReleaseMajor(before, release.Version) {
			return Result{}, status_error.RuntimeMaintenanceUnsupported
		}
		if order >= 0 {
			return Result{Binary: owner.entry, Version: before, SourceURL: sources[id].URL}, nil
		}
		// Pin the official version so the native installer stays on the same major.
		command = append(append([]string(nil), command...), release.Version)
	}
	current := i.officialUpdater(id, owner.entry, nil)
	currentIdentity, statErr := os.Stat(owner.target)
	if current == nil || current.target != owner.target || current.executable != owner.executable || statErr != nil ||
		!os.SameFile(identity, currentIdentity) || currentIdentity.ModTime() != identity.ModTime() ||
		currentIdentity.Size() != identity.Size() {
		return Result{}, failure("official update target changed", statErr)
	}
	if notify != nil {
		notify(Progress{Phase: "updating"})
	}
	if _, err := i.runner.Run(ctx, Command{Binary: owner.executable, Args: command, Dir: owner.home, Env: env}); err != nil {
		if ctx.Err() != nil {
			return Result{}, canceled(ctx)
		}
		return Result{}, failure("official update command", err)
	}
	if notify != nil {
		notify(Progress{Phase: "verifying"})
	}
	if updated := i.officialUpdater(id, owner.entry, nil); updated == nil || updated.executable != owner.executable {
		return Result{}, failure("could not resolve updated CLI", nil)
	}
	after, err := version()
	if err != nil {
		return Result{}, err
	}
	if order, known := CompareVersions(id, before, after); !known || order >= 0 {
		return Result{}, failure("updated CLI version did not advance", nil)
	}
	return Result{Binary: owner.entry, Version: after, SourceURL: sources[id].URL}, nil
}
