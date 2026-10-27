package workspacefiles

import (
	"context"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestDirectoryImportAndResolve(t *testing.T) {
	m, s, root := fixture(t)
	os.Mkdir(filepath.Join(root, "folder"), 0700)
	if _, e := m.Import("web:one", s.ID, "", "folder", "a.txt", []byte("new")); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Import("web:one", s.ID, "", "folder", "a.txt", nil); e != status_error.WorkspaceResourceConflict {
		t.Fatal(e)
	}
	if _, e := m.Import("web:one", s.ID, "", "folder", "../bad", nil); e != status_error.WorkspacePathInvalid {
		t.Fatal(e)
	}
	r, e := m.ResolveReference("web:one", s.ID, "folder/a.txt", root)
	if e != nil || r.Status != "resolved" {
		t.Fatalf("%+v %v", r, e)
	}
	if _, e = m.RenameDirectory("web:one", s.ID, "", "folder", "renamed"); e != nil {
		t.Fatal(e)
	}
	if e = m.DeleteDirectory("web:one", s.ID, "", ""); e != status_error.WorkspacePathInvalid {
		t.Fatal(e)
	}
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "keep"), []byte("safe"), 0600)
	os.Symlink(outside, filepath.Join(root, "renamed", "outside"))
	if e = m.DeleteDirectory("web:one", s.ID, "", "renamed"); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(outside, "keep")); e != nil {
		t.Fatal("followed external link", e)
	}
	r, e = m.ResolveReference("web:one", s.ID, filepath.Join(outside, "keep"), root)
	if e != nil || r.Status != "outside_scope" {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestChatFilesPreserveProjectsAndRestoreAttachments(t *testing.T) {
	m, s, root := fixture(t)
	list, e := m.ListChatFiles(context.Background(), "web:one", s.ID, "")
	if e != nil || len(list.Generated) != 0 {
		t.Fatalf("%+v %v", list, e)
	}
	os.MkdirAll(filepath.Join(root, "attachments/generated"), 0700)
	os.WriteFile(filepath.Join(root, "attachments/generated/out.txt"), []byte("out"), 0600)
	if e = m.DeleteChatFile("web:one", s.ID, "", "note.txt"); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(root, "note.txt")); e != nil {
		t.Fatal("project removed", e)
	}
	if e = m.DeleteChatFile("web:one", s.ID, "", "attachments/generated/out.txt"); e != nil {
		t.Fatal(e)
	}
	list, e = m.ListChatFiles(context.Background(), "web:one", s.ID, "")
	if e != nil || len(list.RemovedPaths) != 2 || len(list.Generated) != 0 {
		t.Fatalf("%+v %v", list, e)
	}
	os.WriteFile(filepath.Join(root, "attachments/generated/out.txt"), []byte("again"), 0600)
	list, e = m.ListChatFiles(context.Background(), "web:one", s.ID, "")
	if e != nil || len(list.RemovedPaths) != 1 || len(list.Generated) != 1 {
		t.Fatalf("%+v %v", list, e)
	}
	os.Remove(filepath.Join(root, removedManifest))
	os.Symlink(filepath.Join(root, "note.txt"), filepath.Join(root, removedManifest))
	if e = m.DeleteChatFile("web:one", s.ID, "", "note.txt"); e != status_error.WorkspacePathInvalid {
		t.Fatal(e)
	}
}
func TestWatchFileAndRootDirectoryCancellation(t *testing.T) {
	m, s, root := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready, events, closeWatch, e := m.Watch(ctx, "web:one", s.ID, "note.txt", "", false)
	if e != nil {
		t.Fatal(e)
	}
	defer closeWatch()
	if ready.Sequence != 1 || ready.Resource == nil {
		t.Fatal(ready)
	}
	os.WriteFile(filepath.Join(root, "note.txt"), []byte("changed"), 0600)
	select {
	case event := <-events:
		if event.Sequence <= 1 || event.Resource == nil || event.Resource.Version == ready.Resource.Version {
			t.Fatal(event)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("missing file event")
	}
	_, events2, close2, e := m.Watch(ctx, "web:one", s.ID, "", "", true)
	if e != nil {
		t.Fatal(e)
	}
	defer close2()
	os.WriteFile(filepath.Join(root, "new.txt"), []byte("new"), 0600)
	select {
	case event := <-events2:
		if event.State != "changed" && event.State != "recovering" {
			t.Fatal(event)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("missing directory event")
	}
	cancel()
	close2()
	closeWatch()
}

func TestDirectoryMutationCannotRemoveAccountParent(t *testing.T) {
	m, s, root := fixture(t)
	if err := os.MkdirAll(filepath.Join(root, "runtime/accounts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteDirectory("web:one", s.ID, "", "runtime"); err != status_error.WorkspacePathInvalid {
		t.Fatalf("account parent delete: %v", err)
	}
	if _, err := m.RenameDirectory("web:one", s.ID, "", "runtime", "moved"); err != status_error.WorkspacePathInvalid {
		t.Fatalf("account parent rename: %v", err)
	}
}

func TestWatchLargeDirectoryUsesBoundedDescriptors(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS 递归监听的文件描述符回归")
	}
	m, scope, root := fixture(t)
	for index := range 512 {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("file-%d.txt", index)), []byte("old"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	count := func() int {
		t.Helper()
		directory, err := os.Open("/dev/fd")
		if err != nil {
			t.Fatal(err)
		}
		defer directory.Close()
		files, err := directory.Readdirnames(-1)
		if err != nil {
			t.Fatal(err)
		}
		return len(files)
	}
	before := count()
	_, events, stop, err := m.Watch(context.Background(), "web:one", scope.ID, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if growth := count() - before; growth > 64 {
		t.Fatalf("512 个文件导致监听新增 %d 个描述符，应与文件数无关", growth)
	}
	if err := os.WriteFile(filepath.Join(root, "file-256.txt"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event.State != "changed" && event.State != "recovering" {
			t.Fatal(event)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("已有文件内容修改未触发变化")
	}
	stop()
	if growth := count() - before; growth > 32 {
		t.Fatalf("停止观察后仍额外保留 %d 个描述符", growth)
	}
}

func TestWatchSharesPhysicalPathAcrossScopesAndReleasesLastSubscriber(t *testing.T) {
	m, first, root := fixture(t)
	second, err := m.Open("web:two", root, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, stopFirst, err := m.Watch(context.Background(), "web:one", first.ID, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer stopFirst()
	_, changes, stopSecond, err := m.Watch(context.Background(), "web:two", second.ID, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer stopSecond()
	physicalCount := func() int {
		m.watchMu.Lock()
		defer m.watchMu.Unlock()
		return len(m.watches)
	}
	if count := physicalCount(); count != 1 {
		t.Fatalf("同一真实目录创建了 %d 份物理监听", count)
	}
	if _, _, _, err := m.Watch(context.Background(), "web:one", second.ID, "", "", true); err != status_error.WorkspaceScopeInvalid {
		t.Fatalf("共享监听绕过授权: %v", err)
	}
	stopFirst()
	if physicalCount() != 1 {
		t.Fatal("一个消费者退出提前释放了共享监听")
	}
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("still observed"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-changes:
		if event.State != "changed" && event.State != "recovering" {
			t.Fatal(event)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("剩余消费者未收到变化")
	}
	stopSecond()
	if physicalCount() != 0 {
		t.Fatal("最后一个消费者退出后仍保留物理监听")
	}
}

func TestWatchSlowConsumerReceivesLatestSnapshotWithoutFalseRecovery(t *testing.T) {
	m, scope, root := fixture(t)
	_, events, stop, err := m.Watch(context.Background(), "web:one", scope.ID, "note.txt", "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	// 暂停消费，让监听只积压一个失效信号，而不是逐次快照。
	time.Sleep(50 * time.Millisecond)
	for index := range 20 {
		if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte(fmt.Sprintf("latest-%d", index)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	expected, _, err := m.Read("web:one", scope.ID, "", "note.txt")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	sequence := uint64(1)
	for {
		select {
		case event := <-events:
			if event.State != "changed" || event.Sequence != sequence+1 {
				t.Fatalf("慢消费者产生了错误恢复状态或序号跳跃: %+v", event)
			}
			sequence = event.Sequence
			if event.Resource != nil && event.Resource.Version == expected.Version {
				return
			}
		case <-deadline.C:
			t.Fatal("未读到最后一次文件变化的快照")
		}
	}
}
