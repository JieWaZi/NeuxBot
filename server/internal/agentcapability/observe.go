package agentcapability

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
)

// DecodeParams 只转换已经通过操作 Schema 的参数，不允许再添加调用身份。
func DecodeParams(params map[string]any, target any) error {
	data, err := json.Marshal(params)
	if err != nil || json.Unmarshal(data, target) != nil {
		return status_error.AgentInputInvalid
	}
	return nil
}

// Page 按明确上限切出当前资源列表，不将一页误称为完整覆盖。
func Page[T any](items []T, params map[string]any) (map[string]any, error) {
	var query struct {
		// Cursor 是上一页明确返回的偏移位置。
		Cursor string `json:"cursor"`
		// PageNum 是从一开始的页码。
		PageNum int `json:"pageNum"`
		// PageSize 是有界页面大小。
		PageSize int `json:"pageSize"`
	}
	if err := DecodeParams(params, &query); err != nil {
		return nil, err
	}
	if query.PageNum == 0 {
		query.PageNum = 1
	}
	if query.PageSize == 0 {
		query.PageSize = 50
	}
	if query.PageNum < 1 || query.PageSize < 1 || query.PageSize > 100 || query.PageNum > 1_000_000 {
		return nil, status_error.AgentInputInvalid
	}
	start := (query.PageNum - 1) * query.PageSize
	if query.Cursor != "" {
		if _, explicit := params["pageNum"]; explicit {
			return nil, status_error.AgentInputInvalid
		}
		value, err := strconv.Atoi(query.Cursor)
		if err != nil || value < 0 || value > 100_000_000 {
			return nil, status_error.AgentInputInvalid
		}
		start = value
	}
	if start > len(items) {
		start = len(items)
	}
	end := min(start+query.PageSize, len(items))
	next := ""
	if end < len(items) {
		next = strconv.Itoa(end)
	}
	return map[string]any{"nextCursor": next, "items": items[start:end], "total": len(items), "pageNum": query.PageNum, "pageSize": query.PageSize}, nil
}

// Observe 先建立精确监听，再读取权威状态；退出、期限和缓冲失效均有明确语义。
func Observe(ctx context.Context, params map[string]any, subscribe func() events.Subscription, read func(context.Context) (any, bool, error)) (Result, error) {
	var input struct {
		// TimeoutMS 是本次观察期限，不是业务运行期限。
		TimeoutMS int64 `json:"timeoutMs"`
	}
	if err := DecodeParams(params, &input); err != nil {
		return Result{}, err
	}
	if input.TimeoutMS == 0 {
		input.TimeoutMS = 30000
	}
	if input.TimeoutMS < 1 || input.TimeoutMS > 60000 {
		return Result{}, status_error.AgentInputInvalid
	}
	subscription := subscribe()
	defer subscription.Close()
	timer := time.NewTimer(time.Duration(input.TimeoutMS) * time.Millisecond)
	defer timer.Stop()
	value, completed, err := read(ctx)
	if err != nil {
		return Result{}, err
	}
	for !completed {
		select {
		case <-ctx.Done():
			return Result{}, status_error.AgentContextUnauthorized
		case <-timer.C:
			return Result{Data: map[string]any{"completed": false, "resource": value}}, nil
		case _, ok := <-subscription.Events:
			if !ok {
				return Result{}, status_error.AgentConnectionUnavailable
			}
			value, completed, err = read(ctx)
			if err != nil {
				return Result{}, err
			}
		}
	}
	return Result{Data: map[string]any{"completed": true, "resource": value}}, nil
}
