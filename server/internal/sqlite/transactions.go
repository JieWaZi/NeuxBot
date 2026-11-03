// Package sqlite 维护业务 SQLite 连接的事务清理边界。
package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"gorm.io/gorm"
)

// ProtectTransactions 在数据库初始化时安装事务连接保护，不改变连接池配置或 SQLite 模式。
func ProtectTransactions(database *gorm.DB) error {
	if database.Dialector.Name() != "sqlite" {
		return nil
	}
	if _, protected := database.ConnPool.(*transactionPool); protected {
		return nil
	}
	pool, err := database.DB()
	if err != nil {
		logx.Errorf("read sqlite connection pool failed: error=%v", err)
		return status_error.DatabaseError
	}
	protected := &transactionPool{DB: pool}
	database.ConnPool = protected
	database.Statement.ConnPool = protected
	return nil
}

// transactionPool 保留现有连接池，直到事务失败后的底层清理完成才归还物理连接。
type transactionPool struct {
	// DB 是原有数据库连接池，查询和池配置继续由其维护。
	*sql.DB
}

// GetDBConn 让 GORM 的数据库读取和关闭入口继续使用原有连接池。
func (p *transactionPool) GetDBConn() (*sql.DB, error) { return p.DB, nil }

// BeginTx 为事务固定一条连接，防止失败清理前被下一次查询复用。
func (p *transactionPool) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	connection, err := p.DB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := connection.BeginTx(ctx, options)
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	return &protectedTransaction{Tx: tx, connection: connection}, nil
}

// protectedTransaction 防止 Commit 失败但仍处于 SQLite 事务中的连接重新入池。
type protectedTransaction struct {
	// Tx 执行原有 SQL，并保留 database/sql 的取消和事务互斥规则。
	*sql.Tx
	// connection 固定本次事务的物理连接，供失败后清理。
	connection *sql.Conn
	// terminal 串行结束、补偿清理和归池，避免迟到 Rollback 提前关闭连接。
	terminal sync.Mutex
	// released 保证 GORM 的迟到 Rollback 不会关闭后续使用者的连接。
	released sync.Once
}

// Commit 保留原始提交结果，并在失败后清理或丢弃仍然独占的连接。
func (t *protectedTransaction) Commit() error {
	t.terminal.Lock()
	defer t.terminal.Unlock()
	defer t.release()
	err := t.Tx.Commit()
	if err != nil && !errors.Is(err, sql.ErrTxDone) {
		// database/sql 已将 Tx 标记为结束，不能再通过 Tx.Rollback 清理。
		// 独占连接仍未归池；清理不继承已取消的业务 Context。
		if _, cleanupErr := t.connection.ExecContext(context.Background(), "ROLLBACK"); cleanupErr != nil {
			logx.Errorf("rollback sqlite connection after failed commit: error=%v", cleanupErr)
			t.discard()
		}
	}
	return err
}

// Rollback 回滚原事务；清理失败的连接不得返回连接池。
func (t *protectedTransaction) Rollback() error {
	t.terminal.Lock()
	defer t.terminal.Unlock()
	defer t.release()
	err := t.Tx.Rollback()
	if err != nil && !errors.Is(err, sql.ErrTxDone) {
		t.discard()
	}
	return err
}

func (t *protectedTransaction) discard() {
	// ErrBadConn 使 database/sql 关闭物理连接，不能将清理失败的连接留给下一次事务。
	_ = t.connection.Raw(func(any) error { return driver.ErrBadConn })
}

func (t *protectedTransaction) release() {
	t.released.Do(func() { _ = t.connection.Close() })
}
