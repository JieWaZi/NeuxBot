package cliinstall

import (
	"errors"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func userExecutablePath() (string, error) {
	localData, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	if err != nil {
		return "", installFailure("find local app data", "", err)
	}
	return filepath.Join(localData, "NeuxBot", "bin", "botcli.exe"), nil
}

func publishExecutable(source, target string, replace bool) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	flags := uint32(windows.MOVEFILE_WRITE_THROUGH)
	if replace {
		flags |= windows.MOVEFILE_REPLACE_EXISTING
	}
	return windows.MoveFileEx(from, to, flags)
}

func configureUserPath(directory string) error {
	const environmentKey = `HKEY_CURRENT_USER\Environment`
	key, _, err := registry.CreateKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return installFailure("open user environment", environmentKey, err)
	}
	defer key.Close()
	current, kind, err := key.GetStringValue("Path")
	if errors.Is(err, registry.ErrNotExist) {
		kind = registry.EXPAND_SZ
	} else if err != nil {
		return installFailure("read user PATH", environmentKey, err)
	}
	updated, changed := appendWindowsPath(current, directory, kind == registry.EXPAND_SZ)
	if !changed {
		return nil
	}
	if kind == registry.EXPAND_SZ {
		err = key.SetExpandStringValue("Path", updated)
	} else {
		err = key.SetStringValue("Path", updated)
	}
	if err != nil {
		return installFailure("update user PATH", environmentKey, err)
	}
	notifyEnvironmentChanged()
	return nil
}

func appendWindowsPath(current, directory string, expandable bool) (string, bool) {
	for entry := range strings.SplitSeq(current, ";") {
		entry = strings.Trim(strings.TrimSpace(entry), `"`)
		if expandable {
			if expanded, err := registry.ExpandString(entry); err == nil {
				entry = expanded
			}
		}
		if strings.EqualFold(filepath.Clean(entry), filepath.Clean(directory)) {
			return current, false
		}
	}
	if current == "" {
		return directory, true
	}
	return current + ";" + directory, true
}

func notifyEnvironmentChanged() {
	const (
		hwndBroadcast   = 0xffff
		wmSettingChange = 0x1a
		smtoAbortIfHung = 0x2
		timeoutMillis   = 100
	)
	notify := windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")
	if notify.Find() != nil {
		return
	}
	environment, _ := windows.UTF16PtrFromString("Environment")
	_, _, _ = notify.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(environment)), smtoAbortIfHung, timeoutMillis, 0)
}
