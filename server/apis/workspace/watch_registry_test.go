package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchRegistryOwnershipAndLifetime(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	scope, err := files.Open("web:one", root, nil)
	if err != nil {
		t.Fatal(err)
	}
	lifetime, revoke := context.WithCancel(context.Background())
	defer revoke()
	id, err := registerWatch(lifetime, "web:one", WatchRequest{ScopeID: scope.ID, RelativePath: "a.txt", Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = SubscribeWatch(context.Background(), "web:two", id); err == nil {
		t.Fatal("other subject received file watch")
	}
	stream, closeWatch, err := SubscribeWatch(context.Background(), "web:one", id)
	if err != nil {
		t.Fatal(err)
	}
	defer closeWatch()
	select {
	case ready := <-stream:
		if ready.SubscriptionID != id || ready.Resource == nil || ready.Sequence != 1 {
			t.Fatalf("bad ready: %+v", ready)
		}
	case <-time.After(time.Second):
		t.Fatal("watch not ready")
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case changed := <-stream:
		if changed.Sequence <= 1 {
			t.Fatal("non-monotonic change")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("external change not delivered")
	}
	revoke()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		watchRegistry.Lock()
		exists := watchRegistry.values[id] != nil
		watchRegistry.Unlock()
		if !exists {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("revoked session retained watch")
}
