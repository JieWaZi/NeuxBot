package controllers

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestDatabaseBusyBackoffAndExhaustion(t *testing.T) {
	for _, succeeds := range []bool{false, true} {
		attempts := 0
		var delays []time.Duration
		err := retryDatabase(context.Background(), func() error {
			attempts++
			if succeeds && attempts == 4 {
				return nil
			}
			return status_error.DatabaseBusy
		}, func(_ context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			return nil
		})
		if attempts != 4 || !reflect.DeepEqual(delays, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}) {
			t.Fatalf("attempts=%d delays=%v", attempts, delays)
		}
		if succeeds && err != nil || !succeeds && !errors.Is(err, status_error.DatabaseBusy) {
			t.Fatalf("succeeds=%v error=%v", succeeds, err)
		}
	}
}

func TestDatabaseRetryMapsRollbackAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	err := retryDatabase(ctx, func() error { cancel(); return sql.ErrTxDone }, func(context.Context, time.Duration) error { t.Fatal("cancelled query must not retry"); return nil })
	if !errors.Is(err, status_error.DatabaseOperationCancelled) {
		t.Fatalf("cancelled rollback became database failure: %v", err)
	}
}

func TestDatabaseRetryCancellationAndNonBusy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	err := retryDatabase(ctx, func() error {
		attempts++
		cancel()
		return status_error.DatabaseBusy
	}, waitDatabaseRetry)
	if !errors.Is(err, status_error.DatabaseOperationCancelled) || attempts != 1 {
		t.Fatalf("cancelled attempts=%d error=%v", attempts, err)
	}
	attempts = 0
	err = retryDatabase(context.Background(), func() error {
		attempts++
		return status_error.WakerNameConflict
	}, func(context.Context, time.Duration) error {
		t.Fatal("non-busy error must not wait")
		return nil
	})
	if attempts != 1 || !errors.Is(err, status_error.WakerNameConflict) {
		t.Fatalf("non-busy attempts=%d error=%v", attempts, err)
	}
}

func TestSQLiteConcurrentTransactionsRetryFreshSnapshots(t *testing.T) {
	db := newTestDB(t)
	wakers := NewWakerController(db)
	transactions := NewTransactionController(dbhelper.NewGormTransactionManager(db))
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	ids := []string{"retry-first", "retry-second"}
	for _, id := range ids {
		if err := wakers.Create(ctx, models.Waker{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	ready := make(chan struct{}, 2)
	proceed := make(chan struct{})
	errorsOut := make(chan error, 2)
	var calls sync.WaitGroup
	for _, id := range ids {
		calls.Add(1)
		go func(id string) {
			defer calls.Done()
			first := true
			errorsOut <- transactions.Transaction(ctx, func(txctx context.Context) error {
				waker, err := wakers.Get(txctx, id)
				if err != nil {
					return err
				}
				if first {
					first = false
					ready <- struct{}{}
					select {
					case <-proceed:
					case <-ctx.Done():
						return status_error.DatabaseOperationCancelled
					}
				}
				waker.Name += "-updated"
				return wakers.Update(txctx, id, waker)
			})
		}(id)
	}
	for range ids {
		select {
		case <-ready:
		case <-ctx.Done():
			t.Fatal("concurrent reads did not complete")
		}
	}
	close(proceed)
	calls.Wait()
	for range ids {
		if err := <-errorsOut; err != nil {
			t.Fatalf("concurrent transaction: %v", err)
		}
	}
	for _, id := range ids {
		waker, err := wakers.Get(ctx, id)
		if err != nil || waker.Name != id+"-updated" {
			t.Fatalf("transaction must apply exactly once: id=%s name=%s error=%v", id, waker.Name, err)
		}
	}
}

func TestSQLiteStandaloneWriteRetriesWithoutHoldingTransaction(t *testing.T) {
	db := newTestDB(t)
	wakers := NewWakerController(db)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	lock := db.WithContext(ctx).Begin()
	if lock.Error != nil {
		t.Fatal(lock.Error)
	}
	defer lock.Rollback()
	if err := lock.Create(&models.Waker{ID: "held", Name: "held"}).Error; err != nil {
		t.Fatal(err)
	}
	released := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		released <- lock.Commit().Error
	}()
	if err := wakers.Create(ctx, models.Waker{ID: "waiting", Name: "waiting"}); err != nil {
		t.Fatalf("standalone busy write should retry: %v", err)
	}
	if err := <-released; err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteNestedBusyRollsBackAndRetriesOuterTransaction(t *testing.T) {
	db := newTestDB(t)
	wakers := NewWakerController(db)
	transactions := NewTransactionController(dbhelper.NewGormTransactionManager(db))
	outerCalls, innerCalls := 0, 0
	err := transactions.Transaction(context.Background(), func(ctx context.Context) error {
		outerCalls++
		if err := wakers.Create(ctx, models.Waker{ID: "outer-retry", Name: "outer-retry"}); err != nil {
			return err
		}
		return transactions.Transaction(ctx, func(context.Context) error {
			innerCalls++
			if outerCalls == 1 {
				return status_error.DatabaseBusy
			}
			return nil
		})
	})
	if err != nil || outerCalls != 2 || innerCalls != 2 {
		t.Fatalf("outer=%d inner=%d error=%v", outerCalls, innerCalls, err)
	}
	if _, err := wakers.Get(context.Background(), "outer-retry"); err != nil {
		t.Fatalf("only the committed attempt should persist: %v", err)
	}
}

func TestSQLiteStandaloneReadRetriesBusyWhileScanningRows(t *testing.T) {
	db := newTestDB(t)
	wakers := NewWakerController(db)
	ctx := context.Background()
	if err := wakers.Create(ctx, models.Waker{ID: "busy-reader", Name: "busy-reader"}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	writer, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	reader, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// 缓存 reader 的 Schema，使 prepare 成功，真实 busy 出现在 rows.Next。
	var cachedName string
	if err := reader.QueryRowContext(ctx, "SELECT name FROM wakers WHERE id = ?", "busy-reader").Scan(&cachedName); err != nil {
		reader.Close()
		t.Fatal(err)
	}
	reader.Close()
	if _, err := writer.ExecContext(ctx, "BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	defer writer.ExecContext(ctx, "ROLLBACK")
	released := make(chan error, 1)
	go func() {
		// 超过驱动自身默认等待，确保至少发生一次真实 busy，再在重试期间释放。
		time.Sleep(6500 * time.Millisecond)
		_, err := writer.ExecContext(ctx, "COMMIT")
		released <- err
	}()
	value, readErr := wakers.Get(ctx, "busy-reader")
	if err := <-released; err != nil {
		t.Fatal(err)
	}
	if readErr != nil || value.Name != "busy-reader" {
		t.Fatalf("scan busy must retry the complete query: name=%s error=%v", value.Name, readErr)
	}
}
