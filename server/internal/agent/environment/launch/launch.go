// Package launch 统一原生智能体启动与认证资源生命周期。
package launch

import (
	"context"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/driver/acp/native"
	"github.com/JieWaZi/NeuxBot/server/internal/codexoauth"
	acp "github.com/JieWaZi/acp-go/sdk"
)

// Config 冻结原生启动事实与已有账号权威的资源端口，不读取业务数据库。
type Config struct {
	// Native 是产品已经确认的原生程序、参数与工作目录。
	Native native.Config
	// UseNativeAuth 明确由原生认证文件提供系统账号凭据；无刷新回调不隐含系统身份。
	UseNativeAuth bool
	// Refresh 只向既有账号凭据权威请求令牌轮换。
	Refresh codexoauth.RefreshFunc
	// Release 在原生消费者退出及认证对账完成后释放账号与受管目录，失败可重试。
	Release func(context.Context) error
}

// PreparedAgent 持有一次原生消费者的启动与认证资源，不拥有原生协议会话。
type PreparedAgent struct {
	// native 保存原生启动事实与准备阶段的认证环境。
	native native.Config
	// environment 是按最后覆盖值冻结的准备阶段环境，任务资源可在启动前追加授权贡献。
	environment map[string]string
	// broker 是当前消费者独有的刷新入口。
	broker *codexRefreshBroker
	// sourceAuth 是消费者退出后需要对账的系统认证权威文件。
	sourceAuth string
	// release 由产品资源所有者提供，成功之前持续保留。
	release func(context.Context) error
	// gate 串行化创建与关闭并允许调用方取消等待。
	gate *codexoauth.CredentialGate
	// preparationError 阻止部分准备失败后启动原生消费者。
	preparationError error
	// closing 禁止关闭开始后重新创建原生消费者。
	closing bool
	// closed 标记全部资源已成功释放，使后续关闭无需再次接纳上下文。
	closed atomic.Bool
}

// Prepare 取得当前消费者的认证资源；失败仍交回持有部分资源的可关闭所有者。
func Prepare(ctx context.Context, config Config) (prepared *PreparedAgent, err error) {
	values := make(map[string]string, len(config.Native.Environment))
	for _, pair := range config.Native.Environment {
		if key, value, ok := strings.Cut(pair, "="); ok {
			values[key] = value
		}
	}
	// 刷新能力地址只能由当前所有者提供，不能继承已结束消费者的端点。
	delete(values, codexRefreshURLVariable)
	config.Native.PrefixArgs = slices.Clone(config.Native.PrefixArgs)
	config.Native.CodexConfiguration = maps.Clone(config.Native.CodexConfiguration)
	config.Native.NativePluginDirectories = slices.Clone(config.Native.NativePluginDirectories)
	prepared = &PreparedAgent{native: config.Native, environment: values, release: config.Release, gate: codexoauth.NewCredentialGate()}
	defer func() {
		prepared.native.Environment = prepared.Environment()
		prepared.preparationError = err
	}()
	if err := ctx.Err(); err != nil {
		return prepared, err
	}
	if config.Native.RuntimeID != constants.RuntimeIDCodex {
		return prepared, nil
	}
	refresh := config.Refresh
	if refresh == nil && config.UseNativeAuth {
		path, err := codexAuthPathForEnvironment(prepared.Environment())
		if err != nil {
			return prepared, err
		}
		refresh, err = codexFileRefresh(path, nil)
		if err != nil {
			return prepared, err
		}
		prepared.sourceAuth = path
	}
	if refresh != nil {
		prepared.broker, err = newCodexRefreshBroker(refresh)
		if err != nil {
			return prepared, err
		}
		prepared.environment[codexRefreshURLVariable] = prepared.broker.URL()
	}
	return prepared, nil
}

// New 从同一启动租约创建原生适配器，冻结资源物化后交付的最终授权环境；nil 使用准备快照。
// 调用方先关闭原生消费者，再关闭本资源所有者。
func (p *PreparedAgent) New(ctx context.Context, environment map[string]string) (acp.Agent, error) {
	unlock, err := p.gate.Lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if p.preparationError != nil {
		return nil, p.preparationError
	}
	if p.closing {
		return nil, status_error.RuntimeUnavailable
	}
	values := maps.Clone(environment)
	if values == nil {
		values = maps.Clone(p.environment)
	}
	// 刷新入口始终属于本租约，任务贡献不能替换或继承其他消费者的端点。
	delete(values, codexRefreshURLVariable)
	if p.broker != nil {
		values[codexRefreshURLVariable] = p.broker.URL()
	}
	config := p.native
	config.Environment = orderedEnvironment(values)
	return native.New(ctx, config)
}

// Credential 向内核交付准备阶段的独立环境副本，资源贡献在启动前合并后交给 New。
func (p *PreparedAgent) Credential() c.Credential {
	return c.Credential{Environment: maps.Clone(p.environment)}
}

// Environment 返回按键稳定排序的准备环境副本，不允许调用方修改已冻结事实。
func (p *PreparedAgent) Environment() []string {
	return orderedEnvironment(p.environment)
}

func orderedEnvironment(environment map[string]string) []string {
	keys := make([]string, 0, len(environment))
	for key := range environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, key+"="+environment[key])
	}
	return values
}

// Close 停止刷新并等待持久化，然后对账系统凭据及释放账号；每一失败保留原所有者。
func (p *PreparedAgent) Close(ctx context.Context) error {
	if p.closed.Load() {
		return nil
	}
	unlock, err := p.gate.Lock(ctx)
	if err != nil {
		if p.closed.Load() {
			return nil
		}
		return err
	}
	defer unlock()
	p.closing = true
	if p.broker != nil {
		if err := p.broker.Close(ctx); err != nil {
			return err
		}
		p.broker = nil
	}
	if p.sourceAuth != "" {
		unlockFile, err := codexFileCredentialGate(p.sourceAuth).Lock(ctx)
		if err != nil {
			return err
		}
		_, err = reconcileCodexFileAuth(p.sourceAuth)
		unlockFile()
		if err != nil {
			return err
		}
		p.sourceAuth = ""
	}
	if p.release != nil {
		if err := p.release(ctx); err != nil {
			return err
		}
		p.release = nil
	}
	p.closed.Store(true)
	return nil
}
