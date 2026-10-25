package events

import (
	"context"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	eventhub "github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"testing"
)

// subscriptionConnections 仅模拟连接元数据，不接入本地知识SDK或执行端口。
type subscriptionConnections struct {
	// KnowledgeConnectionController 保持其他未调用方法的端口形状。
	controllers.KnowledgeConnectionController
	// enabled 是当前连接启用事实。
	enabled bool
	// calls 记录实际元数据校验次数。
	calls int
}

func (s *subscriptionConnections) Get(context.Context, string) (models.KnowledgeConnection, error) {
	s.calls++
	return models.KnowledgeConnection{ID: "remote", Backend: "custom", Enabled: s.enabled}, nil
}
func TestRemoteKnowledgeSubscriptionUsesConnectionWithoutLocalEngine(t *testing.T) {
	old, oldEngine := global.KnowledgeConnectionControllerManager, global.KnowledgeEngine
	defer func() { global.KnowledgeConnectionControllerManager = old; global.KnowledgeEngine = oldEngine }()
	stub := &subscriptionConnections{enabled: true}
	global.KnowledgeConnectionControllerManager = stub
	global.KnowledgeEngine = nil
	command := control{Topic: "knowledge", Resource: eventhub.Resource{Kind: "knowledge_library", ID: "native/id", ParentID: "remote"}}
	if err := validateResource(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	if stub.calls != 1 {
		t.Fatalf("connection calls=%d", stub.calls)
	}
	stub.enabled = false
	if err := validateResource(context.Background(), command); !errors.Is(err, status_error.KnowledgeForbidden) {
		t.Fatalf("disabled subscription=%v", err)
	}
}
