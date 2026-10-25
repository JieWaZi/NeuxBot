package controllers

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// fakeTransactionManager 为事务边界测试返回预设错误。
type fakeTransactionManager struct {
	// err 是模拟 begin、commit 或回调失败时返回的错误。
	err error
}

// Transaction 返回预设错误，不执行真实数据库事务。
func (f fakeTransactionManager) Transaction(
	_ context.Context,
	_ func(ctx context.Context) error,
) error {
	return f.err
}

func TestTransactionControllerPreservesStatusError(t *testing.T) {
	controller := NewTransactionController(fakeTransactionManager{
		err: status_error.WakerNameConflict,
	})
	err := controller.Transaction(context.Background(), func(context.Context) error {
		return nil
	})
	if !errors.Is(err, status_error.WakerNameConflict) {
		t.Fatalf("transaction error = %v, want WakerNameConflict", err)
	}
}

func TestTransactionControllerMapsInfrastructureError(t *testing.T) {
	controller := NewTransactionController(fakeTransactionManager{
		err: errors.New("commit failed"),
	})
	err := controller.Transaction(context.Background(), func(context.Context) error {
		return nil
	})
	if !errors.Is(err, status_error.DatabaseError) {
		t.Fatalf("transaction error = %v, want DatabaseError", err)
	}
}
