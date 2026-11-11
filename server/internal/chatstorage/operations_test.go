package chatstorage

import (
	"errors"
	"sync"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestSharedAdmissionsExcludeWakerDeletionWithoutSerializingChats(t *testing.T) {
	root := t.TempDir()
	first, err := AcquireAdmission(root, "partner", "chat-one")
	if err != nil {
		t.Fatal(err)
	}
	second, err := AcquireAdmission(root, "partner", "chat-two")
	if err != nil {
		t.Fatalf("same Waker chats cannot prepare together: %v", err)
	}
	if _, err := AcquireOperation(root, "waker:partner"); !errors.Is(err, status_error.ChatHasActiveExecution) {
		t.Fatalf("delete escaped active admissions: %v", err)
	}
	if _, err := AcquireAdmission(root, "partner", "chat-one"); !errors.Is(err, status_error.ChatHasActiveExecution) {
		t.Fatalf("same Chat was admitted twice: %v", err)
	}
	first()
	first()
	if _, err := AcquireOperation(root, "waker:partner"); !errors.Is(err, status_error.ChatHasActiveExecution) {
		t.Fatalf("duplicate release dropped another Chat's admission: %v", err)
	}
	second()
	writer, err := AcquireOperation(root, "waker:partner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireAdmission(root, "partner", "chat-three"); !errors.Is(err, status_error.ChatHasActiveExecution) {
		t.Fatalf("admitted during deletion: %v", err)
	}
	writer()
	writer()
	release, err := AcquireAdmission(root, "partner", "chat-three")
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestConcurrentChatAdmissionsRetainOneWakerDeletionGate(t *testing.T) {
	root := t.TempDir()
	const count = 16
	ready := make(chan error, count)
	leave := make(chan struct{})
	var completed sync.WaitGroup
	for i := 0; i < count; i++ {
		completed.Add(1)
		go func(id int) {
			defer completed.Done()
			release, err := AcquireAdmission(root, "partner", string(rune('a'+id)))
			ready <- err
			if err == nil {
				<-leave
				release()
			}
		}(i)
	}
	for i := 0; i < count; i++ {
		if err := <-ready; err != nil {
			t.Errorf("parallel admission rejected: %v", err)
		}
	}
	if _, err := AcquireOperation(root, "waker:partner"); !errors.Is(err, status_error.ChatHasActiveExecution) {
		t.Errorf("delete escaped concurrent readers: %v", err)
	}
	close(leave)
	completed.Wait()
	release, err := AcquireOperation(root, "waker:partner")
	if err != nil {
		t.Fatal(err)
	}
	release()
}
