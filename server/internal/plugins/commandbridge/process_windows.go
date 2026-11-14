//go:build windows

package commandbridge

import (
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// configureProcessGroup 在宿主持有进程树之前暂停入口线程。
func configureProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED | windows.CREATE_NEW_PROCESS_GROUP}
}

// manageProcessTree 让宿主持有的 Job 句柄覆盖整个进程树。
func manageProcessTree(command *exec.Cmd) (func(), error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	kill := func() { _ = windows.CloseHandle(job) }
	settings := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&settings)), uint32(unsafe.Sizeof(settings))); err != nil {
		kill()
		return nil, err
	}
	pid := uint32(command.Process.Pid)
	if err := assignAndResume(job, pid); err != nil {
		kill()
		return nil, err
	}
	return kill, nil
}

func assignAndResume(job windows.Handle, pid uint32) error {
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		return err
	}
	threads, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(threads)
	first := windows.ThreadEntry32{}
	first.Size = uint32(unsafe.Sizeof(first))
	for err := windows.Thread32First(threads, &first); err == nil; err = windows.Thread32Next(threads, &first) {
		if first.OwnerProcessID != pid {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, first.ThreadID)
		if err != nil {
			return err
		}
		defer windows.CloseHandle(thread)
		_, err = windows.ResumeThread(thread)
		return err
	}
	return windows.ERROR_NOT_FOUND
}

// Windows 先用 EOF 通知桥接退出，关闭 Job 句柄后系统强制回收剩余进程。
func terminateProcessGroup(*exec.Cmd) {}
