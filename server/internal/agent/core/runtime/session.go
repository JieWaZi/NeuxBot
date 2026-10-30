package runtime

import (
	"context"

	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/internal/plan"
)

// 会话接纳、原生准备和资源释放共用 Runtime 的唯一接纳锁与所有权集合。
type session struct {
	grants        c.Grants                      // grants 是两阶段会话实际接纳的宿主授权上界。
	id            string                        // id 是引擎分配的会话身份。
	revision      uint64                        // revision 每次接纳回合递增。
	driver        c.DriverSession               // driver 是唯一拥有的原生会话。
	closers       []func(context.Context) error // closers 按取得顺序保存会话资源释放函数。
	call          *invocation                   // call 是当前唯一调用，空值表示闲置。
	closing       bool                          // closing 禁止显式关闭期间再次接纳。
	cleanupBusy   bool                          // cleanupBusy 表示某次清理独占会话释放栈。
	managed       bool                          // managed 表示由两阶段接口管理的工作区会话。
	released      bool                          // released 保留已关闭会话的兼容事实，避免资源变更后误恢复旧原生上下文。
	requestKey    string                        // requestKey 是物化前静态声明的兼容摘要。
	nativeHandle  string                        // nativeHandle 是最近真实回执确认的原生身份。
	resources     c.ResourceLease               // resources 是唯一持有的宿主资源租约。
	openDriver    c.Driver                      // openDriver 是两阶段会话独占的原生驱动。
	credential    c.Credential                  // credential 是仅供原生准备的秘密。
	caps          c.DriverCapabilities          // caps 是本会话固定的技术能力。
	declared      c.SessionPlan                 // declared 是宿主最初冻结的配置，决定下轮请求兼容。
	directive     c.SessionDirective            // directive 是实际资源确定的新建或恢复操作。
	plan          plan.Plan                     // plan 是实际资源对应的冻结描述。
	tags          []string                      // tags 标记本会话保护的资源身份。
	closeAfterRun bool                          // closeAfterRun 要求返回终态前释放资源。
}

func validateResources(contributions c.Contributions, resources []c.ResourceBinding) error {
	bindings := make(map[string]c.ResourceBinding, len(resources))
	for _, binding := range resources {
		if binding.ID == "" || (binding.Path == "") == (binding.MCP == nil) {
			return fault(c.FaultProtocol, "resource", "invalid resource binding", nil)
		}
		if _, ok := bindings[binding.ID]; ok {
			return fault(c.FaultPreparation, "resource", "conflicting resource binding", nil)
		}
		bindings[binding.ID] = binding
	}
	for _, skill := range contributions.Skills {
		if bindings[skill.ResourceID].Path == "" && !skill.Optional {
			return fault(c.FaultPreparation, "resource", "required skill resource missing", nil)
		}
	}
	for _, mcp := range contributions.MCP {
		if bindings[mcp.ResourceID].MCP == nil && !mcp.Optional {
			return fault(c.FaultPreparation, "resource", "required mcp resource missing", nil)
		}
	}
	return nil
}

func (e *Runtime) releaseInvocationSession(call *invocation) *c.Fault {
	sess := call.session
	e.mu.Lock()
	sess.closing = true
	e.beginSessionCleanup(sess)
	e.mu.Unlock()
	cleanup := e.releaseSession(sess)
	e.mu.Lock()
	e.finishSessionCleanup(sess, cleanup)
	e.mu.Unlock()
	return cleanup
}

// beginSessionCleanup 与 finishSessionCleanup 均由接纳锁保护。
func (e *Runtime) beginSessionCleanup(sess *session) {
	sess.cleanupBusy = true
}
func (e *Runtime) finishSessionCleanup(sess *session, cleanup *c.Fault) {
	sess.cleanupBusy = false
	sess.call = nil
	if cleanup == nil {
		if sess.managed && !e.closing {
			sess.released = true
			sess.closing = false
			sess.driver = nil
			sess.resources = nil
			sess.openDriver = nil
			sess.credential = c.Credential{}
		} else {
			delete(e.sessions, sess.id)
		}
	}
}
func (e *Runtime) releaseSession(sess *session) *c.Fault {
	ctx, cancel := context.WithTimeout(context.Background(), e.config.CleanupTimeout)
	defer cancel()
	return e.releaseSessionWith(ctx, sess)
}
func (e *Runtime) releaseSessionWith(ctx context.Context, sess *session) *c.Fault {
	for len(sess.closers) > 0 {
		last := len(sess.closers) - 1
		// 只有成功才证明此使用者已停止；失败保留栈顶及其依赖，供显式重试。
		if err := sess.closers[last](ctx); err != nil {
			return fault(c.FaultCleanup, "cleanup", "session resource release incomplete; dependencies retained", err)
		}
		sess.closers[last] = nil
		sess.closers = sess.closers[:last]
	}
	return nil
}
