package events

import (
	"context"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/apis/workspace"
)

// newWorkspaceSource 把受限文件观察复用到现有全局事件通道。
func newWorkspaceSource(ctx context.Context, subject string, command control) (source, error) {
	changes, closeWatch, err := workspace.SubscribeWatch(ctx, subject, command.Resource.ID)
	if err != nil {
		return source{}, err
	}
	return source{close: closeWatch, run: func(ctx context.Context, emit func(frame, bool) bool) {
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-changes:
				if !ok {
					return
				}
				payload, err := json.Marshal(event)
				if err != nil {
					return
				}
				if !emit(frame{Type: "workspace_changed", Payload: payload}, false) {
					return
				}
			}
		}
	}}, nil
}
