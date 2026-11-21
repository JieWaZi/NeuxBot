package commandbridge

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
)

// Selection 只允许同一 Authorizer 在发起管理动作时使用选中的固定上下文。
type Selection struct {
	// Authorizer 是与执行占用及其他版本共享的身份所有者。
	*authorization.Authorizer
	// expected 是指定管理动作已校验的版本和账号代次。
	expected OperationContext
}

// SelectOperation 将管理动作与已核验版本绑定。
func SelectOperation(owner *authorization.Authorizer, expected OperationContext) *Selection {
	return &Selection{Authorizer: owner, expected: expected}
}

func (s *Selection) matches(candidate authorization.Driver) bool {
	driver, ok := candidate.(ContextDriver)
	return ok && driver.OperationContext() == s.expected
}

// Login 在共享锁内核对驱动并开始唯一浏览器或包命令授权。
func (s *Selection) Login(ctx context.Context, scopes ...string) (authorization.Session, error) {
	return s.Authorizer.LoginFor(ctx, s.matches, scopes...)
}

// Status 固定驱动后执行管理请求的检查。
func (s *Selection) Status(ctx context.Context, verify bool) (authorization.Account, error) {
	return s.Authorizer.StatusFor(ctx, s.matches, verify)
}

// Logout 固定驱动后清理共享身份。
func (s *Selection) Logout(ctx context.Context) error {
	return s.Authorizer.LogoutFor(ctx, s.matches)
}

// GuardReferences 固定驱动后阻止新活跃引用，以清理最后身份引用。
func (s *Selection) GuardReferences() (*authorization.ReferenceGuard, error) {
	return s.Authorizer.GuardReferencesFor(s.matches)
}
