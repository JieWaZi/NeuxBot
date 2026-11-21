//go:build !windows

package cliinstall

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

const shellPathMarker = "# NeuxBot CLI"

const unixPathScript = `case ":${PATH-}:" in
  *":$HOME/.local/bin:"*) ;;
  *) export PATH="$HOME/.local/bin${PATH:+:$PATH}" ;;
esac
`

const fishPathScript = `if not contains -- "$HOME/.local/bin" $PATH
    set -gx PATH "$HOME/.local/bin" $PATH
end
`

func userExecutablePath() (string, error) {
	home, err := userHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "bin", "botcli"), nil
}

func userHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return "", installFailure("find absolute home directory", home, err)
	}
	return home, nil
}

func publishExecutable(source, target string, replace bool) error {
	if replace {
		return os.Rename(source, target)
	}
	// A new link publishes the complete file only while the target name is unoccupied.
	return os.Link(source, target)
}

func configureUserPath(_ string) error {
	home, err := userHome()
	if err != nil {
		return err
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		if runtime.GOOS == "darwin" {
			shell = "zsh"
		} else {
			shell = "bash"
		}
	}
	return configureShellPath(home, filepath.Base(shell))
}

func configureShellPath(home, shell string) error {
	switch shell {
	case "zsh":
		config := os.Getenv("ZDOTDIR")
		if !filepath.IsAbs(config) {
			config = home
		}
		return appendPathSetup(filepath.Join(config, ".zshrc"), unixPathScript)
	case "bash":
		loginProfile, err := bashLoginProfile(home)
		if err != nil {
			return err
		}
		if err := appendPathSetup(loginProfile, unixPathScript); err != nil {
			return err
		}
		return appendPathSetup(filepath.Join(home, ".bashrc"), unixPathScript)
	case "fish":
		config := os.Getenv("XDG_CONFIG_HOME")
		if !filepath.IsAbs(config) {
			config = filepath.Join(home, ".config")
		}
		directory := filepath.Join(config, "fish")
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return installFailure("create fish config directory", directory, err)
		}
		return appendPathSetup(filepath.Join(directory, "config.fish"), fishPathScript)
	case "sh", "dash", "ksh":
		return appendPathSetup(filepath.Join(home, ".profile"), unixPathScript)
	default:
		logx.Warnf("install botcli: unsupported shell %q", shell)
		return status_error.BotCLIInstallationFailed
	}
}

func bashLoginProfile(home string) (string, error) {
	for _, name := range []string{".bash_profile", ".bash_login"} {
		path := filepath.Join(home, name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", installFailure("inspect bash login profile", path, err)
		}
	}
	return filepath.Join(home, ".profile"), nil
}

func appendPathSetup(path, commands string) error {
	// Follow existing dotfile links in place. O_EXCL on creation leaves a dangling link untouched.
	flags := os.O_RDWR | os.O_APPEND | syscall.O_NONBLOCK
	file, err := os.OpenFile(path, flags, 0)
	if errors.Is(err, os.ErrNotExist) {
		file, err = os.OpenFile(path, flags|os.O_CREATE|os.O_EXCL, 0o600)
	}
	if err != nil {
		return installFailure("open shell profile", path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return installFailure("inspect shell profile", path, err)
	}
	stat, owned := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !owned || int(stat.Uid) != os.Geteuid() {
		logx.Warnf("install botcli: shell profile %q is not a regular file owned by the current user", path)
		return status_error.BotCLIInstallationFailed
	}
	current, err := io.ReadAll(file)
	if err != nil {
		return installFailure("read shell profile", path, err)
	}
	marker := []byte("\n" + shellPathMarker + "\n")
	if bytes.Contains(current, marker) || bytes.HasPrefix(current, marker[1:]) {
		return nil
	}
	if _, err := file.WriteString("\n\n" + shellPathMarker + "\n" + commands); err != nil {
		return installFailure("update shell profile", path, err)
	}
	return nil
}
