package im

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	protocol "github.com/JieWaZi/NeuxBot/server/internal/im"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/statuserror"
)

func workflowCapabilityRun(t *testing.T) models.WorkflowRun {
	t.Helper()
	run := models.WorkflowRun{ID: util.New(), DefinitionID: util.New()}
	workspace, err := workflowfiles.PrepareWorkspace(t.TempDir(), run.DefinitionID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(workspace)
	var value map[string]any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	run.Context = map[string]any{"workspace": value}
	run.ScopeBinding.WorkingDirectory, err = filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func TestWorkflowDeliveryReadsInternalAndBusinessFilesWithoutExpandingScope(t *testing.T) {
	for _, location := range []string{"internal", "business", "outside", "symlink", "relative", "relative-business-only"} {
		t.Run(location, func(t *testing.T) {
			s, _, platform := newIMFixture(t)
			run := workflowCapabilityRun(t)
			workspace := run.Context["workspace"].(map[string]any)
			path := filepath.Join(workspace["runDirectory"].(string), "report.txt")
			outside := filepath.Join(t.TempDir(), "private.txt")
			if location == "business" || location == "relative-business-only" {
				path = filepath.Join(run.ScopeBinding.WorkingDirectory, "report.txt")
			} else if location == "outside" {
				path = outside
			}
			if location == "symlink" {
				if err := os.WriteFile(outside, []byte("private"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte("真实报告"), 0o600); err != nil {
				t.Fatal(err)
			}
			reference := path
			if location == "relative" {
				if err := os.WriteFile(filepath.Join(run.ScopeBinding.WorkingDirectory, "report.txt"), []byte("旧业务报告"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if strings.HasPrefix(location, "relative") {
				reference = "report.txt"
			}
			call := models.WorkflowActionCall{ID: "file-call", ToolName: constants.IMBuiltinToolName, Input: map[string]any{"action": constants.IMActionCreateDelivery, "targetId": "b", "files": []string{reference}}}
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			_, err := s.WorkflowCall(ctx, run, call)
			if location == "outside" || location == "symlink" || location == "relative-business-only" {
				var detail *statuserror.StatusErr
				if !errors.As(err, &detail) || detail.Fields["reason"] != status_error.IMFileInvalid.Key() {
					t.Fatalf("out-of-scope file did not fail at the file boundary: %v", err)
				}
				_, total, lookupErr := s.config.Store.Deliveries(context.Background(), "", 50, 0)
				if lookupErr != nil || total != 0 || len(platform.ids) != 0 {
					t.Fatal("invalid file registered or sent a delivery")
				}
				return
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("authorized file was rejected before durable delivery: %v", err)
			}
			deliveries, total, err := s.config.Store.Deliveries(context.Background(), "", 50, 0)
			if err != nil || total != 1 || len(deliveries[0].Files) != 1 {
				t.Fatalf("file delivery not durably frozen: %+v %v", deliveries, err)
			}
			body, err := os.ReadFile(deliveries[0].Files[0].Path)
			if err != nil || string(body) != "真实报告" {
				t.Fatalf("delivery did not retain actual file bytes: %q %v", body, err)
			}
			if err := s.sendDelivery(context.Background(), deliveries[0]); err != nil {
				t.Fatal(err)
			}
			if _, err := s.WorkflowCall(context.Background(), run, call); err != nil || len(platform.ids) != 1 {
				t.Fatalf("frozen file delivery could not settle once: %v %+v", err, platform.ids)
			}
		})
	}
}

func TestWorkflowDeliveryRejectsMissingFrozenWorkspace(t *testing.T) {
	s, _, platform := newIMFixture(t)
	run := workflowCapabilityRun(t)
	run.Context = nil
	_, err := s.WorkflowCall(context.Background(), run, models.WorkflowActionCall{ID: "missing-workspace", ToolName: constants.IMBuiltinToolName, Input: map[string]any{"action": constants.IMActionCreateDelivery, "targetId": "b", "content": "未授权运行"}})
	var failure interface{ Key() string }
	if !errors.As(err, &failure) || failure.Key() != status_error.WorkflowStateInvalid.Key() {
		t.Fatalf("IM delivery fell back to the business scope without a workspace: %v", err)
	}
	_, total, err := s.config.Store.Deliveries(context.Background(), "", 50, 0)
	if err != nil || total != 0 || len(platform.ids) != 0 {
		t.Fatal("invalid run registered or sent a delivery")
	}
}

func TestWorkflowTargetFailuresKeepSpecificCauseAndNeverRegisterDelivery(t *testing.T) {
	for _, scenario := range []string{"missing", "revoked", "disabled_delivery", "disabled_connection", "signed_out"} {
		t.Run(scenario, func(t *testing.T) {
			s, _, platform := newIMFixture(t)
			ctx := context.Background()
			target := "b"
			expected := status_error.IMResourceNotFound
			binding, _ := s.config.Store.Binding(ctx, "b")
			switch scenario {
			case "missing":
				target = "deleted"
			case "revoked":
				binding.State = constants.IMBindingStateRejected
				expected = status_error.IMTargetNotAuthorized
			case "disabled_delivery":
				binding.AllowWorkflowDelivery = false
				expected = status_error.IMWorkflowDeliveryNotAllowed
			case "disabled_connection":
				connection, _ := s.config.Store.Connection(ctx, "c")
				connection.Enabled = false
				if err := s.config.Store.SaveConnection(ctx, connection, connection.Revision); err != nil {
					t.Fatal(err)
				}
				expected = status_error.IMConnectionDisabled
			case "signed_out":
				if err := s.config.Store.ConnectionState(ctx, "c", constants.IMConnectionStateLoginRequired, ""); err != nil {
					t.Fatal(err)
				}
				expected = status_error.IMLoginRequired
			}
			if scenario == "revoked" || scenario == "disabled_delivery" {
				if err := s.config.Store.SaveBinding(ctx, binding, binding.Revision); err != nil {
					t.Fatal(err)
				}
			}
			_, err := s.WorkflowCall(ctx, workflowCapabilityRun(t), models.WorkflowActionCall{ID: "call", ToolName: constants.IMBuiltinToolName, Input: map[string]any{"action": "deliveries.create", "targetId": target, "content": "正文"}})
			var detail *statuserror.StatusErr
			if !errors.As(err, &detail) || detail.Fields["reason"] != expected.Key() || detail.Params["cause"] == nil {
				t.Fatalf("错误丢失具体原因: %v", err)
			}
			_, total, err := s.config.Store.Deliveries(ctx, "", 50, 0)
			if err != nil || total != 0 || len(platform.ids) != 0 {
				t.Fatal("失败校验触发了平台发送")
			}
		})
	}
}

func TestWorkflowDeliveryPermissionChangeStopsPendingMessage(t *testing.T) {
	s, _, platform := newIMFixture(t)
	ctx := context.Background()
	delivery, err := s.CreateDelivery(ctx, DeliveryRequest{RequestID: "pending", TargetID: "b", Content: "正文", WorkflowRunID: "run"})
	if err != nil {
		t.Fatal(err)
	}
	binding, _ := s.config.Store.Binding(ctx, "b")
	binding.AllowWorkflowDelivery = false
	if err := s.config.Store.SaveBinding(ctx, binding, binding.Revision); err != nil {
		t.Fatal(err)
	}
	if err := s.sendDelivery(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	result, err := s.config.Store.Delivery(ctx, delivery.ID)
	if err != nil || result.State != constants.IMDeliveryStateFailed || result.ErrorCode != status_error.IMWorkflowDeliveryNotAllowed.Key() || len(platform.ids) != 0 {
		t.Fatalf("关闭发送权限后仍派发: %+v %v", result, err)
	}
}

func TestWorkflowPermissionRevocationKeepsSentPartsAndStopsFollowingParts(t *testing.T) {
	s, _, platform := newIMFixture(t)
	ctx := context.Background()
	platform.afterSend = func() {
		binding, err := s.config.Store.Binding(ctx, "b")
		if err != nil {
			t.Fatal(err)
		}
		binding.AllowWorkflowDelivery = false
		if err := s.config.Store.SaveBinding(ctx, binding, binding.Revision); err != nil {
			t.Fatal(err)
		}
	}
	delivery, err := s.CreateDelivery(ctx, DeliveryRequest{RequestID: "parts", TargetID: "b", Content: strings.Repeat("消息", 2000), WorkflowRunID: "run"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.sendDelivery(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	result, err := s.config.Store.Delivery(ctx, delivery.ID)
	if err != nil || result.State != constants.IMDeliveryStateFailed || result.NextPart != 1 || len(result.Receipts) != 1 || len(platform.ids) != 1 {
		t.Fatalf("撤销后继续发送或丢失已有收据: %+v %v", result, err)
	}
}

// TestDiagnosticConnectorOnlyDiscoversToolsAndRevokesLease 验证诊断无法调用业务动作且授权独立撤销。
func TestWorkflowDeliveryReconcilesOriginalCallWithoutResending(t *testing.T) {
	s, _, platform := newIMFixture(t)
	ctx := context.Background()
	run := workflowCapabilityRun(t)
	call := models.WorkflowActionCall{ID: "action-call", ToolName: constants.IMBuiltinToolName, Input: map[string]any{"action": "deliveries.create", "targetId": "b", "content": "冻结工作流结果"}}
	// 先模拟 Runner 在等待终态时退出：登记的独立交付保留供同一调用恢复。
	wait, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	pending, err := s.WorkflowCall(wait, run, call)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if summary, ok := pending.(map[string]any); !ok || summary["deliveryId"] == "" {
		t.Fatal("等待超时丢失原投递身份", pending)
	}
	deliveries, total, err := s.config.Store.Deliveries(ctx, constants.IMDeliveryStatePending, 50, 0)
	if err != nil || total != 1 {
		t.Fatalf("original registration missing: %d %v", total, err)
	}
	if err := s.sendDelivery(ctx, deliveries[0]); err != nil {
		t.Fatal(err)
	}
	result, err := s.WorkflowCall(ctx, run, call)
	if err != nil || result.(map[string]any)["state"] != constants.IMDeliveryStateSent || len(platform.ids) != 1 {
		t.Fatalf("recovery resent completed effect: %+v %v %v", result, err, platform.ids)
	}
	final, _ := s.config.Store.Delivery(ctx, deliveries[0].ID)
	if final.WorkflowDefinitionID != run.DefinitionID || final.WorkflowRunID != run.ID {
		t.Fatal("delivery lost workflow source")
	}
	if _, err := s.callCapability(ctx, capabilityLease{chatID: "other", executionID: "other", wakerID: "waker"}, false, "", "", protocol.ToolInput{Action: "deliveries.get", DeliveryID: final.ID}); !errors.Is(err, status_error.IMChatNotAuthorized) {
		t.Fatal("ordinary execution read another run's delivery")
	}
	call.Input = map[string]any{"action": "deliveries.create", "targetId": "b", "content": "changed"}
	if _, err := s.WorkflowCall(ctx, run, call); err == nil || len(platform.ids) != 1 {
		t.Fatal("same workflow call changed frozen payload")
	}
}

func TestWorkflowSentReceiptRecoveryIgnoresLaterPermissionChanges(t *testing.T) {
	for _, scenario := range []string{"revoked", "disabled_delivery", "disabled_connection", "signed_out"} {
		t.Run(scenario, func(t *testing.T) {
			s, _, platform := newIMFixture(t)
			ctx := context.Background()
			run := workflowCapabilityRun(t)
			call := models.WorkflowActionCall{ID: "call", ToolName: constants.IMBuiltinToolName, Input: map[string]any{"action": constants.IMActionCreateDelivery, "targetId": "b", "content": "原结果"}}
			delivery, err := s.CreateDelivery(ctx, DeliveryRequest{RequestID: "tool:" + run.ID + ":call", TargetID: "b", Content: "原结果", WorkflowRunID: run.ID, WorkflowDefinitionID: run.DefinitionID})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.sendDelivery(ctx, delivery); err != nil {
				t.Fatal(err)
			}
			binding, err := s.config.Store.Binding(ctx, "b")
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "revoked", "disabled_delivery":
				binding.State = constants.IMBindingStateRejected
				if scenario == "disabled_delivery" {
					binding.State, binding.AllowWorkflowDelivery = constants.IMBindingStateApproved, false
				}
				err = s.config.Store.SaveBinding(ctx, binding, binding.Revision)
			case "disabled_connection":
				connection, loadErr := s.config.Store.Connection(ctx, "c")
				if loadErr != nil {
					t.Fatal(loadErr)
				}
				connection.Enabled = false
				err = s.config.Store.SaveConnection(ctx, connection, connection.Revision)
			case "signed_out":
				err = s.config.Store.ConnectionState(ctx, "c", constants.IMConnectionStateLoginRequired, "")
			}
			if err != nil {
				t.Fatal(err)
			}
			result, err := s.WorkflowCall(ctx, run, call)
			if err != nil || result.(map[string]any)["state"] != constants.IMDeliveryStateSent || len(platform.ids) != 1 {
				t.Fatalf("恢复已发送调用被当前权限改写或重复发送: %+v %v %v", result, err, platform.ids)
			}
			call.Input = map[string]any{"action": constants.IMActionCreateDelivery, "targetId": "b", "content": "改变的结果"}
			if _, err := s.WorkflowCall(ctx, run, call); err == nil || len(platform.ids) != 1 {
				t.Fatal("恢复允许修改已登记载荷或重复发送")
			}
		})
	}
}

func TestWorkflowPendingReceiptRecoveryCannotBypassRevokedPermission(t *testing.T) {
	s, _, platform := newIMFixture(t)
	ctx := context.Background()
	run := workflowCapabilityRun(t)
	call := models.WorkflowActionCall{ID: "call", ToolName: constants.IMBuiltinToolName, Input: map[string]any{"action": constants.IMActionCreateDelivery, "targetId": "b", "content": "原结果"}}
	delivery, err := s.CreateDelivery(ctx, DeliveryRequest{RequestID: "tool:" + run.ID + ":call", TargetID: "b", Content: "原结果", WorkflowRunID: run.ID, WorkflowDefinitionID: run.DefinitionID})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := s.config.Store.Binding(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	binding.AllowWorkflowDelivery = false
	if err := s.config.Store.SaveBinding(ctx, binding, binding.Revision); err != nil {
		t.Fatal(err)
	}
	if err := s.sendDelivery(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	result, err := s.WorkflowCall(ctx, run, call)
	if err == nil || result.(map[string]any)["state"] != constants.IMDeliveryStateFailed || len(platform.ids) != 0 {
		t.Fatalf("恢复待发送调用绕过新权限: %+v %v %v", result, err, platform.ids)
	}
	_, total, err := s.config.Store.Deliveries(ctx, "", 50, 0)
	if err != nil || total != 1 {
		t.Fatalf("恢复产生新的独立投递: %d %v", total, err)
	}
}

func TestRevokedBindingSettlesPendingDeliveryWithoutCallingPlatform(t *testing.T) {
	s, _, platform := newIMFixture(t)
	ctx := context.Background()
	delivery, err := s.CreateDelivery(ctx, DeliveryRequest{RequestID: "revoke", TargetID: "b", Content: "已生成结果"})
	if err != nil {
		t.Fatal(err)
	}
	binding, _ := s.config.Store.Binding(ctx, "b")
	binding.State = constants.IMBindingStateRejected
	if err := s.config.Store.SaveBinding(ctx, binding, binding.Revision); err != nil {
		t.Fatal(err)
	}
	if err := s.sendDelivery(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	final, _ := s.config.Store.Delivery(ctx, delivery.ID)
	if final.State != constants.IMDeliveryStateFailed || final.ErrorCode != status_error.IMChatNotAuthorized.Key() || len(platform.ids) != 0 || final.Content != delivery.Content {
		t.Fatalf("revocation lost or sent frozen result: %+v", final)
	}
}
