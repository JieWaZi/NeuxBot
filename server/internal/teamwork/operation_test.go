package teamwork

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestAcquireReentersSynchronouslyAndDoesNotBlockOtherTeams(t *testing.T) {
	ctx, release, err := Acquire(context.Background(), "team-a")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	_, nested, err := Acquire(ctx, "team-a")
	if err != nil {
		t.Fatal(err)
	}
	nested()
	_, other, err := Acquire(context.Background(), "team-b")
	if err != nil {
		t.Fatal(err)
	}
	other()
	canceled, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, _, err := Acquire(canceled, "team-a"); !errors.Is(err, status_error.OperationCanceled) {
		t.Fatalf("waiting canceled: %v", err)
	}
}

func TestAcquireWaitsUntilCurrentOperationCompletes(t *testing.T) {
	_, release, err := Acquire(context.Background(), "team-serial")
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() {
		_, next, err := Acquire(context.Background(), "team-serial")
		if err == nil {
			next()
			close(finished)
		}
	}()
	select {
	case <-finished:
		t.Fatal("same team admitted two concurrent operations")
	case <-time.After(20 * time.Millisecond):
	}
	release()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("team waiter did not resume")
	}
}
