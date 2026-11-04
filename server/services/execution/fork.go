package execution

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

// ForkChat reserves the public operation for a native fork owner.
func (m *service) ForkChat(context.Context, chatservice.ForkInput) (string, error) {
	return "", status_error.RuntimePolicyUnsupported
}
