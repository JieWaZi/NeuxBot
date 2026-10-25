package codexoauth

import "context"

// RefreshFunc 向凭据权威刷新指定旧令牌，已轮换时返回最新令牌束。
type RefreshFunc func(context.Context, string) (Tokens, error)

// CredentialGate 仅协调一次凭据读改写，不覆盖模型执行。
type CredentialGate struct {
	// lease 是支持上下文取消的单凭据访问令牌。
	lease chan struct{}
}

// NewCredentialGate 创建不启动后台任务的凭据协调器。
func NewCredentialGate() *CredentialGate { return &CredentialGate{lease: make(chan struct{}, 1)} }

// Lock 等待短期凭据访问；取消不会释放其他调用持有的访问权。
func (g *CredentialGate) Lock(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case g.lease <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-g.lease
			return nil, err
		}
		return func() { <-g.lease }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
