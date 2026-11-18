package storage

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

const observeFileFlags = os.O_RDONLY

func measureVolume(path string) (Disk, error) {
	directory, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return Disk{}, err
	}
	var mount [32768]uint16
	var guid [50]uint16
	err = windows.GetVolumePathName(directory, &mount[0], uint32(len(mount)))
	if err == nil {
		err = windows.GetVolumeNameForVolumeMountPoint(&mount[0], &guid[0], uint32(len(guid)))
	}
	volumeID := windows.UTF16ToString(guid[:])
	if err != nil {
		// SMB 不提供本地卷 GUID，使用实际配置的 UNC 共享身份。
		share := filepath.VolumeName(path)
		if !strings.HasPrefix(share, `\\`) || strings.HasPrefix(share, `\\?\Volume{`) {
			return Disk{}, err
		}
		volumeID = share
	}
	directory, err = windows.UTF16PtrFromString(strings.TrimRight(path, `\`) + `\`)
	if err != nil {
		return Disk{}, err
	}
	result := Disk{ID: strings.ToLower(volumeID)}
	if err := windows.GetDiskFreeSpaceEx(directory, &result.AvailableBytes, &result.TotalBytes, nil); err != nil {
		return Disk{}, err
	}
	return result, nil
}

func filesystemIdentity(file *os.File, _ fs.FileInfo) (fileIdentity, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info); err != nil {
		return fileIdentity{}, err
	}
	inode := uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)
	if inode == 0 {
		return fileIdentity{}, fs.ErrInvalid
	}
	return fileIdentity{volume: uint64(info.VolumeSerialNumber), inode: inode}, nil
}

func regularFileIdentity(root *os.Root, name string, observed fs.FileInfo) (fileIdentity, error) {
	file, err := openObservedFile(root, name, observed)
	if err != nil {
		return fileIdentity{}, err
	}
	defer file.Close()
	return filesystemIdentity(file, observed)
}
