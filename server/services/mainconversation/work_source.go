package mainconversation

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/teamwork"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// ReserveWork 为主体当前有权接单的伙伴和本次授权范围预留工作 Chat。
func (s *Service) ReserveWork(ctx context.Context, kind, id, wakerID, scope string) (models.MainConversationWork, error) {
	return s.reserveWork(ctx, kind, id, wakerID, scope, "")
}

// ReserveExactWork 将已预留的执行来源绑定到当前有权接单的公开主体。
func (s *Service) ReserveExactWork(ctx context.Context, kind, id, wakerID, scope, chatID string) (models.MainConversationWork, error) {
	if len(chatID) != 32 {
		return models.MainConversationWork{}, status_error.BadRequest
	}
	return s.reserveWork(ctx, kind, id, wakerID, scope, chatID)
}

func (s *Service) reserveWork(ctx context.Context, kind, id, wakerID, scope, chatID string) (row models.MainConversationWork, err error) {
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
		if chatID == "" {
			row, e = s.store.ReserveWork(tx, conv.ID, wakerID, scope)
		} else {
			row, e = s.store.ReserveExactWork(tx, conv.ID, wakerID, scope, chatID)
		}
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

// WorkInputTriggers 读取已授权来源的主会话原输入。
func (s *Service) WorkInputTriggers(ctx context.Context, work models.MainConversationWork, inputIDs []string) ([]models.MainConversationMessage, error) {
	return s.store.WorkInputTriggers(ctx, work, inputIDs)
}

// WorkPublications 读取已授权来源的实际公开发言。
func (s *Service) WorkPublications(ctx context.Context, work models.MainConversationWork, executionIDs []string) ([]models.MainConversationMessage, error) {
	return s.store.WorkPublications(ctx, work, executionIDs)
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

// WorkAdmission shares the final source authorization boundary for direct and queued inputs.
func (s *Service) WorkAdmission(kind, id string, work models.MainConversationWork, newAssignment bool,
	admit func(context.Context, func(context.Context) error) error,
) func(context.Context, func(context.Context) error) error {
	if kind != "team" {
		return admit
	}
	return func(ctx context.Context, commit func(context.Context) error) error {
		locked, release, err := teamwork.Acquire(ctx, id)
		if err != nil {
			return err
		}
		defer release()
		if newAssignment {
			team, err := s.teams.Get(locked, id)
			if err != nil {
				return err
			}
			if team.LeadWakerID != work.WakerID {
				return status_error.ChatReplyTargetUnavailable
			}
		}
		chat := models.Chat{ID: work.WorkChatID, WakerID: work.WakerID, HiddenFromWakerHistory: true}
		if err := s.AuthorizeWorkActor(locked, chat); err != nil {
			if util.HasStatusCode(err, status_error.AgentOperationForbidden) {
				return status_error.ChatReplyTargetUnavailable
			}
			return err
		}
		return admit(locked, commit)
	}
}
