package runtimes

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	acp "github.com/JieWaZi/acp-go/sdk"
	"github.com/shrewx/ginx/pkg/statuserror"
)

// ForkSessionRequest 保存分叉所需的原生身份、位置及目标执行环境。
type ForkSessionRequest struct {
	// ReceiptDirectory 是宿主持有的非公开持久操作目录。
	ReceiptDirectory string
	// ReconcileOnly 仅查询已执行原生调用的结果，禁止创建第二个分支。
	ReconcileOnly bool
	// SourceSessionID 是来源的原生句柄，不能交给前端。
	SourceSessionID string
	// Position 是适配器在回合终态返回的不透明定位信息；空值表示末尾。
	Position string
	// SourceWorkspace 是原生会话当前绑定的工作区。
	SourceWorkspace string
	// Run 提供目标工作区、Runtime 配置与权限；不使用消息和历史字段。
	Run RunRequest
}

// ForkSessionResult 是已建立的独立原生分支。
type ForkSessionResult struct {
	// SessionID 是之后续聊和冷恢复使用的新原生句柄。
	SessionID string
	// Mode 是当前实际适配器宣告的分叉精度。
	Mode string
}

// ForkSession 只创建原生分支，不发送模型 Prompt，也不编译复制的历史。
func (sdk *SDK) ForkSession(ctx context.Context, request ForkSessionRequest) (result ForkSessionResult, err error) {
	defer func() { err = normalizeForkError(request, err) }()
	if ctx == nil || request.SourceSessionID == "" {
		return ForkSessionResult{}, status_error.BadRequest
	}
	if err := validateExecutionPolicy(request.Run.Policy.Execution); err != nil {
		return ForkSessionResult{}, err
	}
	prepared, err := sdk.prepareFromDetection(sdk.prepareRequestForTarget(request.Run.Target), request.Run.Detection)
	if err != nil {
		return ForkSessionResult{}, err
	}
	workspace, err := validateWorkspace(request.Run.Workspace)
	if err != nil {
		return ForkSessionResult{}, err
	}
	request.Run.Workspace = workspace
	if sdk.acp == nil {
		return ForkSessionResult{}, status_error.RuntimeUnavailable
	}
	return sdk.acp.ForkSession(ctx, prepared, request)
}

// ForkSession 把统一输入映射到 ACP session/fork，厂商细节全部由适配器承担。
func (backend *acpBackend) ForkSession(ctx context.Context, prepared preparedRuntime, request ForkSessionRequest) (ForkSessionResult, error) {
	binding, err := backend.binding(prepared.definition.baseRuntimeID)
	if err != nil {
		return ForkSessionResult{}, err
	}
	source := request.Run
	source.Workspace.Path = request.SourceWorkspace
	agent, err := newEmbeddedACPAgent(ctx, acpAdapterConfigForManagement(prepared, source, binding), binding)
	if err != nil {
		return ForkSessionResult{}, err
	}
	if err := agent.Start(ctx); err != nil {
		return ForkSessionResult{}, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), acpManagementCloseTimeout)
		defer cancel()
		if err := agent.Close(cleanup); err != nil {
			logx.Errorf("close native fork agent failed: runtime_id=%s error=%v", request.Run.Target.RuntimeID, err)
		}
	}()
	mode := acpmeta.ForkMode(agent.initializeResponse.Meta)
	if agent.initializeResponse.AgentCapabilities.SessionCapabilities.Fork == nil || mode == acpmeta.ForkUnsupported || (request.Position != "" && mode != acpmeta.ForkTurn) {
		return ForkSessionResult{}, status_error.RuntimePolicyUnsupported
	}
	mcp, err := prepareACPMCPConnectors(agent.initializeResponse.AgentCapabilities.McpCapabilities, request.Run.Connectors)
	if err != nil {
		return ForkSessionResult{}, err
	}
	servers, err := acpMCPServers(mcp.compatible)
	if err != nil {
		return ForkSessionResult{}, err
	}
	encoded, err := json.Marshal(servers)
	if err != nil {
		return ForkSessionResult{}, status_error.BadRequest
	}
	var unstableServers []acp.UnstableMcpServer
	if json.Unmarshal(encoded, &unstableServers) != nil {
		return ForkSessionResult{}, status_error.BadRequest
	}
	meta := acpmeta.PositionMetadata(request.Position)
	if meta == nil {
		meta = map[string]any{}
	}
	meta["forkReceiptDirectory"] = request.ReceiptDirectory
	meta["sourceCwd"] = request.SourceWorkspace
	meta["reconcileOnly"] = request.ReconcileOnly
	response, err := agent.connection.UnstableForkSession(ctx, acp.UnstableForkSessionRequest{SessionId: acp.SessionId(request.SourceSessionID), Cwd: request.Run.Workspace.Path, AdditionalDirectories: request.Run.Workspace.AdditionalWritableDirectories, McpServers: unstableServers, Meta: meta})
	if err != nil {
		return ForkSessionResult{}, err
	}
	if response.SessionId == "" || string(response.SessionId) == request.SourceSessionID {
		return ForkSessionResult{}, status_error.RuntimeUnavailable
	}
	if !agent.binding.nativeProcess && agent.initializeResponse.AgentCapabilities.SessionCapabilities.Close != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), acpManagementCloseTimeout)
		defer cancel()
		if _, closeErr := agent.connection.CloseSession(cleanup, acp.CloseSessionRequest{SessionId: response.SessionId}); closeErr != nil {
			logx.Errorf("close fork management session failed: runtime_id=%s error=%v", request.Run.Target.RuntimeID, closeErr)
		}
	}
	return ForkSessionResult{SessionID: string(response.SessionId), Mode: mode}, nil
}

// normalizeForkError 只在 Runtime 外部边界转换原生错误，公开调用者不再重复映射。
func normalizeForkError(request ForkSessionRequest, err error) error {
	if err == nil {
		return nil
	}
	var business statuserror.CommonError
	if errors.As(err, &business) {
		return err
	}
	logx.Errorf("native fork failed: runtime_id=%s reconcile_only=%t error=%v", request.Run.Target.RuntimeID, request.ReconcileOnly, err)
	code := acpFailureCode(err)
	if request.ReconcileOnly || errors.Is(err, acpmeta.ErrForkUnconfirmed) || code == FailureCodeContextCanceled || code == constants.RuntimeFailureCodeRequestTimeout {
		return status_error.ChatForkIndeterminate
	}
	switch code {
	case FailureCodeSessionNotFound:
		return status_error.RuntimeSessionNotFound
	case FailureCodeAuthRequired:
		return status_error.RuntimeAuthRequired
	default:
		return status_error.RuntimeExecutionFailed.WithParams(map[string]interface{}{"Detail": FailureDisplayMessage(err)})
	}
}
