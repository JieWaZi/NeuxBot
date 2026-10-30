package aiaccount

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
)

// closeAccountLease 在测试中核验账号回收结果，避免忽略可重试关闭失败。
func closeAccountLease(t *testing.T, release func(context.Context) error) {
	t.Helper()
	if err := release(context.Background()); err != nil {
		t.Error(err)
	}
}

func TestSubscriptionChatAndKnowledgeShareAccountWhileRunning(t *testing.T) {
	service, account, _ := subscriptionFixture(t)
	_, release, err := service.PrepareQueuedLease(context.Background(), account.ID, constants.RuntimeIDCodex, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer closeAccountLease(t, release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, nextRelease, err := service.PrepareQueuedLease(ctx, account.ID, constants.RuntimeIDCodex, t.TempDir(), "")
	if err != nil {
		t.Fatalf("同账号调用被正在生成的请求阻塞: %v", err)
	}
	if _, err := service.Exclusive(account.ID); !errors.Is(err, status_error.AIAccountBusy) {
		t.Fatal("运行期间允许了账号修改")
	}
	closeAccountLease(t, nextRelease)
}

func TestQueuedLeaseCloseFailureRetainsOwnershipAndCanRetry(t *testing.T) {
	service, account, _ := subscriptionFixture(t)
	workspace := t.TempDir()
	_, closeLease, err := service.PrepareQueuedLease(context.Background(), account.ID, constants.RuntimeIDCodex, workspace, "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(accountconfig.Home(workspace, constants.RuntimeIDCodex), "auth.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("invalid-native-auth"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := closeLease(context.Background()); !errors.Is(err, status_error.AIAccountCredentialError) {
		t.Fatalf("close=%v", err)
	}
	if release, err := service.Exclusive(account.ID); !errors.Is(err, status_error.AIAccountBusy) {
		if release != nil {
			release()
		}
		t.Fatalf("failed cleanup released account ownership: %v", err)
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := closeLease(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := closeLease(context.Background()); err != nil {
		t.Fatal(err)
	}
	release, err := service.Exclusive(account.ID)
	if err != nil {
		t.Fatalf("successful retry retained lease: %v", err)
	}
	release()
}

func TestQueuedPreparationWaitsOnlyForAccountMutationAndCanBeCancelled(t *testing.T) {
	service, account, _ := subscriptionFixture(t)
	release, err := service.Exclusive(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, _, err = service.PrepareQueuedLease(ctx, account.ID, constants.RuntimeIDCodex, t.TempDir(), "")
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, status_error.DatabaseOperationCancelled) {
		t.Fatalf("等待配置修改未响应取消: %v", err)
	}
}
