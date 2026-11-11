package aiaccount

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"github.com/JieWaZi/acp-go/pkg/claude"
)

func TestProgramMaintenanceRetainsIndependentClaudeOwnerUntilConfirmedClose(t *testing.T) {
	var failures atomic.Int32
	failures.Store(1)
	service := &Service{claudeFactory: func(context.Context, claude.Options) (claudeSubscriptionClient, error) {
		return &subscriptionClaudeClient{closeFailures: &failures}, nil
	}}
	client, err := service.newClaudeClient(t.Context(), claude.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.BeginProgramMaintenance(t.Context(), constants.RuntimeIDClaude); !errors.Is(err, status_error.AIAccountBusy) {
		t.Fatalf("maintenance admitted a live observer: %v", err)
	}
	if err := client.Close(t.Context()); !errors.Is(err, status_error.RuntimeShutdownFailed) {
		t.Fatalf("close fixture=%v", err)
	}
	if _, err := service.BeginProgramMaintenance(t.Context(), constants.RuntimeIDClaude); !errors.Is(err, status_error.AIAccountBusy) {
		t.Fatalf("maintenance replaced unclosed observer: %v", err)
	}
	if err := client.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	release, err := service.BeginProgramMaintenance(t.Context(), constants.RuntimeIDClaude)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.newClaudeClient(t.Context(), claude.Options{}); !errors.Is(err, status_error.RuntimeInstallationBusy) {
		t.Fatalf("observer bypassed held maintenance fence: %v", err)
	}
	release()
	release()
	next, err := service.newClaudeClient(t.Context(), claude.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := next.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestProgramMaintenanceRetriesExplicitManagementCleanupOwner(t *testing.T) {
	home := t.TempDir()
	home, _ = filepath.EvalSymlinks(home)
	root := filepath.Join(home, ".claude")
	var failures atomic.Int32
	failures.Store(2)
	created := 0
	service := &Service{claudeFactory: func(context.Context, claude.Options) (claudeSubscriptionClient, error) {
		created++
		return &subscriptionClaudeClient{home: root, closeFailures: &failures, snapshot: accountconfig.ClaudeCredentialSnapshot{AccountID: "fixture", OrganizationID: "fixture-org"}}, nil
	}}
	client, err := service.newClaudeClient(t.Context(), claude.Options{})
	if err != nil {
		t.Fatal(err)
	}
	err = client.Close(t.Context())
	if err != nil {
		service.retainProgramCleanup(constants.RuntimeIDClaude, client)
	}
	if !errors.Is(err, status_error.RuntimeShutdownFailed) {
		t.Fatalf("system observer close=%v", err)
	}
	if _, err := service.BeginProgramMaintenance(t.Context(), constants.RuntimeIDClaude); !errors.Is(err, status_error.RuntimeShutdownFailed) {
		t.Fatalf("failed cleanup lost original owner: %v", err)
	}
	release, err := service.BeginProgramMaintenance(t.Context(), constants.RuntimeIDClaude)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if created != 1 {
		t.Fatalf("cleanup opened replacement status clients: %d", created)
	}
}
