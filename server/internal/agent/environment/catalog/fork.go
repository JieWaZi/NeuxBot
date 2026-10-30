package catalog

import (
	runtimediagnostic "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/diagnostic"

	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	acpdriver "github.com/JieWaZi/NeuxBot/server/internal/agent/core/driver/acp"
	acp "github.com/JieWaZi/acp-go/sdk"

	"context"

	"errors"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"

	"github.com/shrewx/ginx/pkg/statuserror"
)

// ForkRequest 只携带原生分叉所需的启动、工作范围和协议资源事实。
type ForkRequest struct {
	// Target 是已经选择的本地启动与账号配置。
	Target runtimeconfig.RuntimeTarget
	// Detection 是已持久化的有效探测快照。
	Detection runtimeconfig.DetectionResult
	// Workspace 是目标原生会话的工作范围。
	Workspace c.WorkspaceScope
	// ManagedWorkspace 表示宿主管理目标工作区相邻的持久状态。
	ManagedWorkspace bool
	// PermissionLevel 是产品已经授权的执行权限。
	PermissionLevel constants.PermissionLevel
	// MCPServers 是宿主已校验和序列化的协议资源，不带资源领域对象。
	MCPServers []acp.McpServer
	// SourceSessionID 是来源原生句柄。
	SourceSessionID string
	// Position 是原生不透明回合位置。
	Position string
	// SourceWorkspace 是来源原生会话绑定的目录。
	SourceWorkspace string
	// ReceiptDirectory 是宿主持有的私有持久操作目录。
	ReceiptDirectory string
	// ReconcileOnly 只对账已经派发的原生操作。
	ReconcileOnly bool
}

// ForkResult 保存已经建立的原生分支事实。
type ForkResult struct {
	// SessionID 是新原生句柄。
	SessionID string
	// Mode 是原生声明的实际分叉精度。
	Mode string
}

// ForkSession 只创建原生分支，不发送模型 Prompt，也不编译复制的历史。
func (sdk *Catalog) ForkSession(ctx context.Context, request ForkRequest) (result ForkResult, err error) {
	defer func() { err = normalizeForkError(request, err) }()
	if ctx == nil || request.SourceSessionID == "" {
		return ForkResult{}, status_error.BadRequest
	}
	if !constants.SelectablePermissionLevel(request.PermissionLevel) {
		return ForkResult{}, status_error.BadRequest
	}
	prepared, err := sdk.prepareFromDetection(sdk.prepareRequestForTarget(request.Target), request.Detection)
	if err != nil {
		return ForkResult{}, err
	}
	if sdk.acp == nil {
		return ForkResult{}, status_error.RuntimeUnavailable
	}
	return sdk.acp.ForkSession(ctx, prepared, request)
}

// ForkSession 把已冻结宿主参数交给独立管理连接，不提交模型输入。
func (backend *acpBackend) ForkSession(ctx context.Context, prepared preparedRuntime, request ForkRequest) (result ForkResult, err error) {
	binding, err := backend.binding(prepared.definition.baseRuntimeID)
	if err != nil {
		return result, err
	}
	source := c.WorkspaceScope{Root: request.SourceWorkspace}
	owner, err := backend.open(ctx, acpAdapterConfigForManagement(prepared, request.Target, source, request.ManagedWorkspace, request.PermissionLevel, binding), binding, nil)
	defer func() {
		if closeErr := backend.finish(ctx, owner); err == nil {
			err = closeErr
		}
	}()
	if err != nil {
		return result, err
	}
	// 产品资源已由宿主校验；这里仅按当前握手的原生传输能力投影。
	capabilities := owner.manager.InitializeResponse().AgentCapabilities.McpCapabilities
	servers := make([]acp.McpServer, 0, len(request.MCPServers))
	for _, server := range request.MCPServers {
		if server.Stdio != nil || server.Http != nil && capabilities.Http || server.Sse != nil && capabilities.Sse {
			servers = append(servers, server)
		}
	}
	raw, err := owner.manager.Fork(ctx, acpdriver.ForkRequest{SourceSessionID: request.SourceSessionID, Position: request.Position, SourceWorkspace: request.SourceWorkspace, ReceiptDirectory: request.ReceiptDirectory, ReconcileOnly: request.ReconcileOnly, Workspace: request.Workspace, MCPServers: servers, CloseForkSession: !binding.descriptor.NativeProcess})
	return ForkResult{SessionID: raw.SessionID, Mode: raw.Mode}, err
}

// normalizeForkError 只在 Runtime 外部边界转换原生错误，公开调用者不再重复映射。
func normalizeForkError(request ForkRequest, err error) error {
	if err == nil {
		return nil
	}
	var business statuserror.CommonError
	if errors.As(err, &business) {
		return err
	}
	var fault *c.Fault
	if errors.As(err, &fault) && fault.Code == c.FaultUnsupported {
		return status_error.RuntimePolicyUnsupported
	}
	logx.Errorf("native fork failed: runtime_id=%s reconcile_only=%t error=%v", request.Target.RuntimeID, request.ReconcileOnly, err)
	code := runtimediagnostic.ACPFailureCode(err)
	if request.ReconcileOnly || errors.Is(err, acpmeta.ErrForkUnconfirmed) || code == runtimediagnostic.FailureCodeContextCanceled || code == constants.RuntimeFailureCodeRequestTimeout {
		return status_error.ChatForkIndeterminate
	}
	switch code {
	case runtimediagnostic.FailureCodeSessionNotFound:
		return status_error.RuntimeSessionNotFound
	case runtimediagnostic.FailureCodeAuthRequired:
		return status_error.RuntimeAuthRequired
	default:
		return status_error.RuntimeExecutionFailed.WithParams(map[string]interface{}{"Detail": runtimediagnostic.FailureDisplayMessage(err)})
	}
}
