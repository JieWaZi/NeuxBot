package catalog

import (
	c "github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	acpdriver "github.com/JieWaZi/NeuxBot/agent-runtime/driver/acp"

	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"

	"context"

	"errors"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"

	"github.com/shrewx/ginx/pkg/statuserror"
)

// ForkSession 只创建原生分支，不发送模型 Prompt，也不编译复制的历史。
func (sdk *Catalog) ForkSession(ctx context.Context, request runtimecontract.ForkSessionRequest) (result runtimecontract.ForkSessionResult, err error) {
	defer func() { err = normalizeForkError(request, err) }()
	if ctx == nil || request.SourceSessionID == "" {
		return runtimecontract.ForkSessionResult{}, status_error.BadRequest
	}
	if err := runtimecontract.ValidateExecutionPolicy(request.Run.Policy.Execution); err != nil {
		return runtimecontract.ForkSessionResult{}, err
	}
	prepared, err := sdk.prepareFromDetection(sdk.prepareRequestForTarget(request.Run.Target), request.Run.Detection)
	if err != nil {
		return runtimecontract.ForkSessionResult{}, err
	}
	workspace, err := runtimecontract.Validate(request.Run.Workspace)
	if err != nil {
		return runtimecontract.ForkSessionResult{}, err
	}
	request.Run.Workspace = workspace
	if sdk.acp == nil {
		return runtimecontract.ForkSessionResult{}, status_error.RuntimeUnavailable
	}
	return sdk.acp.ForkSession(ctx, prepared, request)
}

// ForkSession 把已冻结宿主参数交给独立管理连接，不提交模型输入。
func (backend *acpBackend) ForkSession(ctx context.Context, prepared preparedRuntime, request runtimecontract.ForkSessionRequest) (result runtimecontract.ForkSessionResult, err error) {
	binding, err := backend.binding(prepared.definition.baseRuntimeID)
	if err != nil {
		return result, err
	}
	source := request.Run
	source.Workspace.Path = request.SourceWorkspace
	owner, err := backend.open(ctx, acpAdapterConfigForManagement(prepared, source, binding), binding, nil)
	defer func() {
		if closeErr := backend.finish(ctx, owner); err == nil {
			err = closeErr
		}
	}()
	if err != nil {
		return result, err
	}
	// 所有连接器仍先经过宿主校验；原生能力限制在独立管理端口确认。
	for _, connector := range request.Run.Connectors {
		if err := runtimecontract.ValidateConnectorConfiguration(connector); err != nil {
			return result, err
		}
	}
	compatible, err := prepareACPMCPConnectors(owner.manager.InitializeResponse().AgentCapabilities.McpCapabilities, request.Run.Connectors)
	if err != nil {
		return result, err
	}
	servers, err := acpMCPServers(compatible.compatible)
	if err != nil {
		return result, err
	}
	raw, err := owner.manager.Fork(ctx, acpdriver.ForkRequest{SourceSessionID: request.SourceSessionID, Position: request.Position, SourceWorkspace: request.SourceWorkspace, ReceiptDirectory: request.ReceiptDirectory, ReconcileOnly: request.ReconcileOnly, Workspace: c.WorkspaceScope{Root: request.Run.Workspace.Path, Directories: request.Run.Workspace.AdditionalWritableDirectories}, MCPServers: servers, CloseForkSession: !binding.nativeProcess})
	return runtimecontract.ForkSessionResult{SessionID: raw.SessionID, Mode: raw.Mode}, err
}

// normalizeForkError 只在 Runtime 外部边界转换原生错误，公开调用者不再重复映射。
func normalizeForkError(request runtimecontract.ForkSessionRequest, err error) error {
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
	logx.Errorf("native fork failed: runtime_id=%s reconcile_only=%t error=%v", request.Run.Target.RuntimeID, request.ReconcileOnly, err)
	code := runtimecontract.ACPFailureCode(err)
	if request.ReconcileOnly || errors.Is(err, acpmeta.ErrForkUnconfirmed) || code == runtimecontract.FailureCodeContextCanceled || code == constants.RuntimeFailureCodeRequestTimeout {
		return status_error.ChatForkIndeterminate
	}
	switch code {
	case runtimecontract.FailureCodeSessionNotFound:
		return status_error.RuntimeSessionNotFound
	case runtimecontract.FailureCodeAuthRequired:
		return status_error.RuntimeAuthRequired
	default:
		return status_error.RuntimeExecutionFailed.WithParams(map[string]interface{}{"Detail": runtimecontract.FailureDisplayMessage(err)})
	}
}
