package mainconversation

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// ReserveWork 为主体当前有权接单的伙伴和本次授权范围预留工作 Chat。
func (s *Service) ReserveWork(ctx context.Context, kind, id, wakerID, scope string) (row models.MainConversationWork, err error) {
	if strings.TrimSpace(scope) == "" || wakerID == "" {
		return row, status_error.BadRequest
	}
	err = s.tx.Transaction(ctx, func(tx context.Context) error {
		actor, e := s.subjectActor(tx, kind, id)
		if e != nil {
			return e
		}
		if actor != wakerID {
			return status_error.AgentOperationForbidden
		}
		conv, e := s.store.Ensure(tx, kind, id)
		if e != nil {
			return e
		}
		row, e = s.store.ReserveWork(tx, conv.ID, wakerID, scope)
		return e
	})
	return
}

// Source 只向本主体主会话提供其工作记录。
func (s *Service) Source(ctx context.Context, kind, id, chatID string) (models.MainConversationWork, error) {
	if _, err := s.subjectActor(ctx, kind, id); err != nil {
		return models.MainConversationWork{}, err
	}
	w, err := s.store.Work(ctx, chatID)
	if err != nil {
		return w, err
	}
	conv, err := s.store.Get(ctx, w.ConversationID)
	if err != nil {
		return models.MainConversationWork{}, err
	}
	if conv.SubjectType != kind || conv.SubjectID != id {
		return models.MainConversationWork{}, status_error.ChatNotFound.WithParams(map[string]interface{}{"ChatID": chatID})
	}
	return w, nil
}

// authorizedWork 确认原执行者仍有权在所属公开会话中使用工具。
func (s *Service) authorizedWork(ctx context.Context, chatID string) (models.MainConversationWork, models.MainConversation, error) {
	w, err := s.store.Work(ctx, chatID)
	if err != nil {
		return w, models.MainConversation{}, err
	}
	conv, err := s.store.Get(ctx, w.ConversationID)
	if err != nil {
		return w, conv, err
	}
	if conv.SubjectType == "team" {
		team, err := s.teams.Get(ctx, conv.SubjectID)
		if err != nil || team.AuthorizesWorkActor(w.WakerID, nil) {
			return w, conv, err
		}
		// 在任成员可完成之前接纳的交办；退出后，工作工具的读写权限同时终止。
		members, err := s.teams.ListMembers(ctx, conv.SubjectID)
		if err != nil || team.AuthorizesWorkActor(w.WakerID, members) {
			return w, conv, err
		}
	} else {
		actor, err := s.subjectActor(ctx, conv.SubjectType, conv.SubjectID)
		if err != nil || actor == w.WakerID {
			return w, conv, err
		}
	}
	return w, conv, status_error.AgentOperationForbidden
}

// ValidateWorkEntry 拒绝从普通聊天和其他入口直接向主会话来源写入执行。
func (s *Service) ValidateWorkEntry(ctx context.Context, chatID string, fromMain bool) error {
	if chatID == "" {
		if fromMain {
			return status_error.AgentOperationForbidden
		}
		return nil
	}
	_, bound, err := s.store.FindWork(ctx, chatID)
	if err != nil {
		return err
	}
	if bound != fromMain {
		return status_error.AgentOperationForbidden
	}
	return nil
}

// AuthorizeWorkActor 核对来源归属与仍有权在该来源执行的原伙伴。
func (s *Service) AuthorizeWorkActor(ctx context.Context, chat models.Chat) error {
	return s.tx.Transaction(ctx, func(tx context.Context) error {
		work, _, err := s.authorizedWork(tx, chat.ID)
		if err != nil {
			return err
		}
		if !chat.HiddenFromWakerHistory || work.WakerID != chat.WakerID {
			return status_error.AgentOperationForbidden
		}
		return nil
	})
}
