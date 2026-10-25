package channel

import (
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestNewReportsChannelInitializationFailureForMissingDependencies(t *testing.T) {
	_, err := New(Config{})
	if !errors.Is(err, status_error.CoreChannelInitializationFailed) {
		t.Fatalf("New(Config{}) error = %v, want CoreChannelInitializationFailed", err)
	}
}
