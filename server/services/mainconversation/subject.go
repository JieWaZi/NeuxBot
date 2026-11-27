// Package mainconversation 管理主体主会话、授权工作来源及明确面向用户的发言。
package mainconversation

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// Service 在接纳和重放时核对同一主体、工作范围与公开目标。
type Service struct {
	// store 保存会话、来源及已发送的消息。
	store controllers.MainConversationStore
	// tx 在同一持久提交中核对主体状态和操作收据。
	tx controllers.TransactionController
	// wakers 提供伙伴的实际身份。
	wakers controllers.WakerController
	// teams 提供团队的当前负责人。
	teams controllers.ChannelController
}

// New 把主会话接到 Core 现有的主体和事务所有者。
func New(store controllers.MainConversationStore, tx controllers.TransactionController, wakers controllers.WakerController, teams controllers.ChannelController) *Service {
	return &Service{store: store, tx: tx, wakers: wakers, teams: teams}
}

// subjectActor 核对主体仍存在并返回当前可开工的实际伙伴。
func (s *Service) subjectActor(ctx context.Context, kind, id string) (string, error) {
	if id == "" {
		return "", status_error.BadRequest
	}
	switch kind {
	case "waker":
		w, err := s.wakers.Get(ctx, id)
		return w.ID, err
	case "team":
		t, err := s.teams.Get(ctx, id)
		return t.LeadWakerID, err
	default:
		return "", status_error.BadRequest
	}
}

// Ensure 返回主体唯一的主会话；主体不存在时不会建立孤立记录。
func (s *Service) Ensure(ctx context.Context, kind, id string) (row models.MainConversation, err error) {
	err = s.tx.Transaction(ctx, func(tx context.Context) error {
		if _, e := s.subjectActor(tx, kind, id); e != nil {
			return e
		}
		var e error
		row, e = s.store.Ensure(tx, kind, id)
		return e
	})
	return
}
