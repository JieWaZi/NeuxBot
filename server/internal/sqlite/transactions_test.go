package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	glebarez "github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestCommitBusyRollsBackBeforeConnectionReuse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "commit-busy.db")
	database, err := gorm.Open(glebarez.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ProtectTransactions(database); err != nil {
		t.Fatal(err)
	}
	pool, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	// 单连接仅用于强制本测试下一次事务复用刚失败的物理连接。
	pool.SetMaxOpenConns(1)
	if err := database.Exec("CREATE TABLE sample(id INTEGER PRIMARY KEY, value TEXT); INSERT INTO sample VALUES(1,'original')").Error; err != nil {
		t.Fatal(err)
	}
	reader, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	readTx, err := reader.Begin()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = readTx.Rollback() })
	var value string
	if err := readTx.QueryRow("SELECT value FROM sample WHERE id=1").Scan(&value); err != nil {
		t.Fatal(err)
	}
	err = database.Transaction(func(tx *gorm.DB) error {
		return tx.Exec("UPDATE sample SET value='updated' WHERE id=1").Error
	})
	var sqliteError interface{ Code() int }
	if !errors.As(err, &sqliteError) || sqliteError.Code()&255 != 5 {
		t.Fatalf("expected real commit contention, got %v", err)
	}
	if err := readTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	err = database.Transaction(func(tx *gorm.DB) error {
		return tx.Raw("SELECT value FROM sample WHERE id=1").Scan(&value).Error
	})
	if err != nil || value != "original" {
		t.Fatalf("failed commit poisoned the pooled connection or leaked writes: value=%q error=%v", value, err)
	}
}

func TestCancelledTransactionLeavesReusableConnection(t *testing.T) {
	database, err := gorm.Open(glebarez.Open(filepath.Join(t.TempDir(), "cancel.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ProtectTransactions(database); err != nil {
		t.Fatal(err)
	}
	pool, _ := database.DB()
	t.Cleanup(func() { _ = pool.Close() })
	pool.SetMaxOpenConns(1)
	if err := database.Exec("CREATE TABLE sample(value TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	err = database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("INSERT INTO sample VALUES('cancelled')").Error; err != nil {
			return err
		}
		cancel()
		return ctx.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation changed: %v", err)
	}
	var count int64
	err = database.Transaction(func(tx *gorm.DB) error {
		return tx.Raw("SELECT count(*) FROM sample").Scan(&count).Error
	})
	if err != nil || count != 0 {
		t.Fatalf("cancelled transaction leaked state: count=%d error=%v", count, err)
	}
}

func TestConcurrentRollbackCannotReleaseConnectionBeforeCommitCleanup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent-finish.db")
	database, err := gorm.Open(glebarez.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ProtectTransactions(database); err != nil {
		t.Fatal(err)
	}
	pool, _ := database.DB()
	t.Cleanup(func() { _ = pool.Close() })
	pool.SetMaxOpenConns(1)
	if err := database.Exec("CREATE TABLE sample(value TEXT); INSERT INTO sample VALUES('original')").Error; err != nil {
		t.Fatal(err)
	}
	reader, _ := sql.Open("sqlite", path)
	t.Cleanup(func() { _ = reader.Close() })
	readTx, err := reader.Begin()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = readTx.Rollback() })
	var value string
	if err := readTx.QueryRow("SELECT value FROM sample").Scan(&value); err != nil {
		t.Fatal(err)
	}
	connPool, err := database.ConnPool.(*transactionPool).BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tx := connPool.(*protectedTransaction)
	if _, err := tx.ExecContext(context.Background(), "UPDATE sample SET value='updated'"); err != nil {
		t.Fatal(err)
	}
	committed := make(chan error, 1)
	go func() { committed <- tx.Commit() }()
	deadline := time.Now().Add(time.Second)
	for {
		_, err := tx.Tx.ExecContext(context.Background(), "SELECT 1")
		if errors.Is(err, sql.ErrTxDone) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("commit did not enter its real driver boundary")
		}
		time.Sleep(time.Millisecond)
	}
	rolledBack := make(chan error, 1)
	go func() { rolledBack <- tx.Rollback() }()
	if err := <-committed; err == nil {
		t.Fatal("reader must block commit")
	}
	if err := <-rolledBack; !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("late rollback changed: %v", err)
	}
	if err := readTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	err = database.Transaction(func(tx *gorm.DB) error { return tx.Raw("SELECT value FROM sample").Scan(&value).Error })
	if err != nil || value != "original" {
		t.Fatalf("concurrent rollback released an unclean connection: value=%q error=%v", value, err)
	}
}

func TestFailedCleanupDiscardsConnection(t *testing.T) {
	database, err := gorm.Open(glebarez.Open(filepath.Join(t.TempDir(), "discard.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ProtectTransactions(database); err != nil {
		t.Fatal(err)
	}
	pool, _ := database.DB()
	t.Cleanup(func() { _ = pool.Close() })
	pool.SetMaxOpenConns(1)
	if err := database.Exec("CREATE TABLE sample(id INTEGER PRIMARY KEY); INSERT INTO sample VALUES(1)").Error; err != nil {
		t.Fatal(err)
	}
	err = database.Transaction(func(tx *gorm.DB) error {
		// SQLite 主动回滚后，COMMIT 与补偿 ROLLBACK 都会失败；连接必须丢弃。
		if tx.Exec("INSERT OR ROLLBACK INTO sample VALUES(1)").Error == nil {
			t.Fatal("constraint must abort the SQLite transaction")
		}
		return nil
	})
	if err == nil || pool.Stats().OpenConnections != 0 {
		t.Fatalf("failed cleanup kept its physical connection: error=%v stats=%+v", err, pool.Stats())
	}
	var count int64
	err = database.Transaction(func(tx *gorm.DB) error {
		return tx.Raw("SELECT count(*) FROM sample").Scan(&count).Error
	})
	if err != nil || count != 1 {
		t.Fatalf("replacement connection lost committed state: count=%d error=%v", count, err)
	}
}

func TestProtectedPoolPreservesNestedSavepointRollback(t *testing.T) {
	database, err := gorm.Open(glebarez.Open(filepath.Join(t.TempDir(), "nested.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ProtectTransactions(database); err != nil {
		t.Fatal(err)
	}
	pool, _ := database.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := database.Exec("CREATE TABLE sample(value TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	err = database.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("INSERT INTO sample VALUES('outer')").Error; err != nil {
			return err
		}
		inner := tx.Transaction(func(nested *gorm.DB) error {
			if err := nested.Exec("INSERT INTO sample VALUES('inner')").Error; err != nil {
				return err
			}
			return status_error.WorkflowStateConflict
		})
		if !errors.Is(inner, status_error.WorkflowStateConflict) {
			t.Fatalf("nested rollback changed the original error: %v", inner)
		}
		return nil
	})
	var values []string
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Raw("SELECT value FROM sample").Scan(&values).Error; err != nil || len(values) != 1 || values[0] != "outer" {
		t.Fatalf("nested rollback changed outer transaction: values=%v error=%v", values, err)
	}
}
