package uploads

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestUploadOwnershipLeaseRetryAndConsumption(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ref, err := s.Stage(context.Background(), "web:a", "note.txt", strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Claim("web:b", []string{ref}); err == nil {
		t.Fatal("cross subject claim")
	}
	if _, _, err = s.Claim("web:a", []string{"/etc/passwd"}); err == nil {
		t.Fatal("arbitrary local path")
	}
	paths, release, err := s.Claim("web:a", []string{ref})
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(paths[0]); err != nil || string(data) != "hello" {
		t.Fatal("wrong bytes", err)
	}
	if _, _, err = s.Claim("web:a", []string{ref}); err == nil {
		t.Fatal("concurrent claim")
	}
	release(false)
	paths, release, err = s.Claim("web:a", []string{ref})
	if err != nil {
		t.Fatal("failed operation cannot retry", err)
	}
	release(true)
	if _, err = os.Stat(paths[0]); !os.IsNotExist(err) {
		t.Fatal("accepted temporary bytes retained")
	}
	if _, _, err = s.Claim("web:a", []string{ref}); err == nil {
		t.Fatal("consumed upload reused")
	}
}

func TestUploadCancellationWaitsForActiveReader(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	life, cancel := context.WithCancel(context.Background())
	ref, err := s.Stage(life, "web:a", "note.txt", strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	paths, release, err := s.Claim("web:a", []string{ref})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	deadline := time.Now().Add(time.Second)
	for {
		s.mu.Lock()
		removed := s.entries[ref].removed
		s.mu.Unlock()
		if removed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("not revoked")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err = os.Stat(paths[0]); err != nil {
		t.Fatal("active reader lost bytes")
	}
	release(false)
	if _, err = os.Stat(paths[0]); !os.IsNotExist(err) {
		t.Fatal("cancelled upload retained")
	}
}

func TestUploadRejectsFileNamesAndUnknownReferences(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, name := range []string{"../secret", "a/b", "a\\b", ".", ""} {
		if _, err = s.Stage(context.Background(), "web:a", name, strings.NewReader("x")); err == nil {
			t.Fatal("invalid filename", name)
		}
	}
	if _, _, err = s.Claim("desktop", []string{"upload:missing/file.txt"}); err == nil {
		t.Fatal("unknown upload")
	}
	paths, release, err := s.Claim("desktop", []string{"/explicit/native.txt"})
	if err != nil || paths[0] != "/explicit/native.txt" {
		t.Fatal(err)
	}
	release(false)
}

func TestAcceptedUploadRetainsOnlySameRequestReplayPath(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ref, err := s.Stage(context.Background(), "web:a", "note.txt", strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	paths, release, err := s.Claim("web:a", []string{ref}, "request-1")
	if err != nil {
		t.Fatal(err)
	}
	release(true)
	if _, err = os.Stat(paths[0]); !os.IsNotExist(err) {
		t.Fatal("accepted bytes retained")
	}
	replay, done, err := s.Claim("web:a", []string{ref}, "request-1")
	if err != nil || replay[0] != paths[0] {
		t.Fatal("idempotent path changed", err)
	}
	done(true)
	if _, _, err = s.Claim("web:a", []string{ref}, "request-2"); err == nil {
		t.Fatal("consumed bytes reused for another request")
	}
	if _, _, err = s.Claim("web:b", []string{ref}, "request-1"); err == nil {
		t.Fatal("replay crosses subject")
	}
}
