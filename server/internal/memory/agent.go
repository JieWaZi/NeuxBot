package memory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

// agentCommandKey 只允许记忆模块在同库事务中附带命令回执。
type agentCommandKey struct{}

// agentCommand 保存一次记忆写入的幂等身份，不复制记忆正文。
type agentCommand struct {
	// id 是首次提交后不再改变的命令身份。
	id string
	// key 是 Chat、伙伴、动作和请求身份的摘要。
	key string
	// hash 是规范输入的摘要，同键异内容必须拒绝。
	hash string
	// memoryID 指向本次真实写入或遗忘的记忆。
	memoryID string
	// duplicate 表示新增内容命中已有知识。
	duplicate bool
	// replayed 表示已找到同一请求的原回执。
	replayed bool
}

func commandDigest(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s *Service) replayAgentCommand(ctx context.Context) (bool, error) {
	command, _ := ctx.Value(agentCommandKey{}).(*agentCommand)
	if command == nil {
		return false, nil
	}
	var previous agentCommand
	err := s.store.db.QueryRowContext(ctx, "SELECT id, request_hash, memory_id, duplicate FROM memory_command_receipts WHERE request_key = ?", command.key).Scan(&previous.id, &previous.hash, &previous.memoryID, &previous.duplicate)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, status_error.DatabaseError
	}
	if previous.hash != command.hash {
		return false, status_error.AgentCommandRequestConflict
	}
	command.id, command.memoryID, command.duplicate, command.replayed = previous.id, previous.memoryID, previous.duplicate, true
	return true, nil
}

func agentMemoryID(ctx context.Context, id string) {
	if command, ok := ctx.Value(agentCommandKey{}).(*agentCommand); ok {
		command.memoryID = id
	}
}

func insertAgentReceipt(ctx context.Context, tx *sql.Tx) error {
	command, _ := ctx.Value(agentCommandKey{}).(*agentCommand)
	if command == nil {
		return nil
	}
	if command.memoryID == "" {
		return status_error.AgentInputInvalid
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO memory_command_receipts (id, request_key, request_hash, memory_id, duplicate) VALUES (?, ?, ?, ?, ?)", command.id, command.key, command.hash, command.memoryID, command.duplicate); err != nil {
		return status_error.DatabaseError
	}
	return nil
}

// AgentBindings 以当前伙伴和项目范围直接调用记忆领域，并在同库提交命令回执。
func (s *Service) AgentBindings(mc Context) []agentcapability.Binding {
	show := func(memory Memory) map[string]any {
		return map[string]any{"memoryId": memory.ID, "scope": memory.Scope, "content": memory.Data, "createdAt": memory.CreatedAt, "updatedAt": memory.UpdatedAt}
	}
	bindings := []agentcapability.Binding{{Operation: "memory.entries.search", Call: func(ctx context.Context, input agentcapability.Request) (agentcapability.Result, error) {
		var params struct {
			// Query 是本次检索文本。
			Query string `json:"query"`
			// Scope 是当前可见范围选择。
			Scope string `json:"scope"`
			// TopK 是有界命中条数。
			TopK int `json:"topK"`
		}
		if err := agentcapability.DecodeParams(input.Params, &params); err != nil {
			return agentcapability.Result{}, err
		}
		result, err := s.Search(ctx, mc, params.Query, constants.MemoryScope(params.Scope), params.TopK)
		if err != nil {
			return agentcapability.Result{}, err
		}
		items := []map[string]any{}
		for _, item := range result.Results {
			value := show(item.Memory)
			value["score"] = item.Score
			items = append(items, value)
		}
		return agentcapability.Result{Data: map[string]any{"items": items, "mode": result.Mode, "indexConflict": result.Conflict}}, nil
	}}, {Operation: "memory.entries.get", Call: func(_ context.Context, input agentcapability.Request) (agentcapability.Result, error) {
		id, _ := input.Params["memoryId"].(string)
		entry, err := s.getAccessibleMemory(id, mc)
		if err != nil {
			return agentcapability.Result{}, err
		}
		return agentcapability.Result{Data: show(*entry)}, nil
	}}}
	for _, operation := range []string{"memory.entries.create", "memory.entries.update", "memory.entries.forget"} {
		newCommand := func(requestID string) *agentCommand {
			return &agentCommand{id: util.New(), key: commandDigest([]string{mc.ChatID, mc.PartnerID, mc.ProjectID, operation, requestID})}
		}
		result := func(command *agentCommand) agentcapability.Result {
			return agentcapability.Result{CommandID: command.id, Replayed: command.replayed, Data: map[string]any{"memoryId": command.memoryID, "duplicate": command.duplicate}}
		}
		bindings = append(bindings, agentcapability.Binding{Operation: operation, Call: func(ctx context.Context, input agentcapability.Request) (agentcapability.Result, error) {
			command := newCommand(input.RequestID)
			command.hash = commandDigest(input.Params)
			ctx = context.WithValue(ctx, agentCommandKey{}, command)
			id, _ := input.Params["memoryId"].(string)
			content, _ := input.Params["content"].(string)
			var err error
			switch operation {
			case "memory.entries.create":
				scope, _ := input.Params["scope"].(string)
				_, err = s.Write(ctx, mc, constants.MemoryScope(scope), content)
			case "memory.entries.update":
				err = s.Update(ctx, mc, id, content)
			case "memory.entries.forget":
				err = s.Forget(ctx, mc, id)
			}
			return result(command), err
		}, Receipt: func(ctx context.Context, requestID string) (agentcapability.Result, error) {
			command := newCommand(requestID)
			err := s.store.db.QueryRowContext(ctx, "SELECT id, request_hash, memory_id, duplicate FROM memory_command_receipts WHERE request_key = ?", command.key).Scan(&command.id, &command.hash, &command.memoryID, &command.duplicate)
			if errors.Is(err, sql.ErrNoRows) {
				return agentcapability.Result{}, status_error.MemoryNotFound
			}
			if err != nil {
				return agentcapability.Result{}, status_error.DatabaseError
			}
			command.replayed = true
			return result(command), nil
		}})
	}
	return bindings
}
