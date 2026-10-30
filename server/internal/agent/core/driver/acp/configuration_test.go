package acp_test

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	driver "github.com/JieWaZi/NeuxBot/server/internal/agent/core/driver/acp"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

// TestPermissionModeRemainsConfirmedAfterLaterReceipts 防止模型或推理回执撤销权限后仍开始 Prompt 并报告原请求模式。
func TestPermissionModeRemainsConfirmedAfterLaterReceipts(t *testing.T) {
	for _, changedBy := range []string{"", "model", "effort"} {
		name := changedBy
		if name == "" {
			name = "confirmed"
		}
		t.Run(name, func(t *testing.T) {
			a := newFixtureAgent()
			options := []sdk.SessionConfigOption{
				selectionOption(sdk.SessionConfigOptionCategoryMode, "mode", "default", "default", "full"),
				selectionOption(sdk.SessionConfigOptionCategoryModel, "model", "old", "old", "requested"),
				selectionOption(sdk.SessionConfigOptionCategoryThoughtLevel, "effort", "low", "low", "high"),
			}
			a.newSession = func(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
				return sdk.NewSessionResponse{SessionId: "native-session", ConfigOptions: options}, nil
			}
			a.configure = func(_ context.Context, request sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
				for _, option := range options {
					if option.Select.Id == request.ValueId.ConfigId {
						option.Select.CurrentValue = request.ValueId.Value
					}
				}
				if string(request.ValueId.ConfigId) == changedBy {
					options[0].Select.CurrentValue = "default"
				}
				return sdk.SetSessionConfigOptionResponse{ConfigOptions: options}, nil
			}
			d := newDriver(t, a)
			request := openRequest(t)
			request.Profile.NativeMode, request.Profile.Model, request.Profile.Reasoning = "full", "requested", "high"
			s, err := d.Open(context.Background(), request)
			if changedBy != "" {
				var fault *contract.Fault
				if !errors.As(err, &fault) || fault.Code != contract.FaultProtocol {
					t.Fatalf("reverted permission was not rejected as protocol fault: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			result := s.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "input"}}, &recordingSink{})
			if result.Configuration.ActualMode != "full" {
				t.Fatalf("actual mode=%q", result.Configuration.ActualMode)
			}
		})
	}
}

// TestConfigurationSelectionKeepsModelThroughReasoningReceipt 捕获推理写入回执撤销已选择模型的错误。
func TestConfigurationSelectionKeepsModelThroughReasoningReceipt(t *testing.T) {
	for _, revert := range []bool{false, true} {
		t.Run(map[bool]string{false: "model remains selected", true: "reasoning reverts selected model"}[revert], func(t *testing.T) {
			a := newFixtureAgent()
			options := []sdk.SessionConfigOption{
				selectionOption(sdk.SessionConfigOptionCategory("speed"), "fast", "false", "false", "true"),
				selectionOption(sdk.SessionConfigOptionCategoryModel, "model", "old", "old", "requested"),
				selectionOption(sdk.SessionConfigOptionCategoryThoughtLevel, "effort", "low", "low", "high"),
			}
			a.newSession = func(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
				return sdk.NewSessionResponse{SessionId: "native-session", ConfigOptions: options}, nil
			}
			a.configure = func(_ context.Context, r sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
				for _, option := range options {
					if option.Select.Id == r.ValueId.ConfigId {
						option.Select.CurrentValue = r.ValueId.Value
					}
				}
				if revert && r.ValueId.ConfigId == "effort" {
					options[1].Select.CurrentValue = "old"
				}
				return sdk.SetSessionConfigOptionResponse{ConfigOptions: options}, nil
			}
			d := newDriver(t, a)
			r := openRequest(t)
			r.Profile.Model = "requested"
			r.Profile.Reasoning = "high"
			s, err := d.Open(context.Background(), r)
			if revert {
				if err == nil {
					t.Fatal("reasoning silently reverted selected model")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			result := s.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "input"}}, &recordingSink{})
			if result.Configuration.ActualModel != "requested" || result.Configuration.ActualReasoning != "high" || options[0].Select.CurrentValue != "false" {
				t.Fatalf("configuration=%+v", result.Configuration)
			}
		})
	}
}

// TestConfigurationSelectionUsesAdvertisedCategory 防止公共选择器按配置 ID 猜测语义或忽略标准模型类别。
func TestConfigurationSelectionUsesAdvertisedCategory(t *testing.T) {
	for _, tt := range []struct {
		// name 描述配置目录中的身份与类别关系。
		name string
		// options 是厂商公布的完整标准目录。
		options []sdk.SessionConfigOption
		// selected 是类别明确声明的真实模型配置身份。
		selected string
	}{
		{"opaque model id", []sdk.SessionConfigOption{selectionOption(sdk.SessionConfigOptionCategoryModel, "reasoning_effort", "old", "old", "requested")}, "reasoning_effort"},
		{"model id with another category", []sdk.SessionConfigOption{
			selectionOption(sdk.SessionConfigOptionCategory("custom"), "model", "off", "off", "on"),
			selectionOption(sdk.SessionConfigOptionCategoryModel, "provider-model", "old", "old", "requested"),
		}, "provider-model"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := newFixtureAgent()
			a.newSession = func(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
				return sdk.NewSessionResponse{SessionId: "native-session", ConfigOptions: tt.options}, nil
			}
			var selected string
			a.configure = func(_ context.Context, request sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
				selected = string(request.ValueId.ConfigId)
				for _, option := range tt.options {
					if option.Select.Id == request.ValueId.ConfigId {
						option.Select.CurrentValue = request.ValueId.Value
					}
				}
				return sdk.SetSessionConfigOptionResponse{ConfigOptions: tt.options}, nil
			}
			d := newDriver(t, a)
			request := openRequest(t)
			request.Profile.Model = "requested"
			s, err := d.Open(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close(context.Background())
			result := s.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "input"}}, &recordingSink{})
			if selected != tt.selected || result.Configuration.ActualModel != "requested" {
				t.Fatalf("selected=%q configuration=%+v", selected, result.Configuration)
			}
		})
	}
}

// TestPermissionModeUsesAdvertisedMenuAndStrictLegacyFallback 防止模式候选和旧版兼容绕过真实权限目录。
func TestPermissionModeUsesAdvertisedMenuAndStrictLegacyFallback(t *testing.T) {
	for _, tt := range []struct {
		name     string            // name 描述原生模式目录。
		modes    []sdk.SessionMode // modes 是当前原生明确声明的目录。
		fallback string            // fallback 是唯一旧模式兼容值。
		want     string            // want 是允许实际选择的模式，空值表示必须拒绝。
	}{{"Qoder fallback", []sdk.SessionMode{{Id: "default"}, {Id: "bypassPermissions"}}, "", "bypassPermissions"}, {"legacy single default", []sdk.SessionMode{{Id: "default"}}, "default", "default"}, {"modern cannot silently use default", []sdk.SessionMode{{Id: "default"}, {Id: "auto"}}, "default", ""}} {
		t.Run(tt.name, func(t *testing.T) {
			a := newFixtureAgent()
			a.newSession = func(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
				return sdk.NewSessionResponse{SessionId: "native-session", Modes: &sdk.SessionModeState{CurrentModeId: "default", AvailableModes: tt.modes}}, nil
			}
			selected := ""
			a.configureMode = func(_ context.Context, r sdk.SetSessionModeRequest) (sdk.SetSessionModeResponse, error) {
				selected = string(r.ModeId)
				return sdk.SetSessionModeResponse{}, nil
			}
			d, err := driver.New(driver.Config{ID: "fixture", ModeCandidates: []string{"yolo", "bypassPermissions"}, SingleModeFallback: tt.fallback, Factory: func(context.Context, contract.OpenRequest) (sdk.Agent, error) { return a, nil }})
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close(context.Background())
			s, err := d.Open(context.Background(), openRequest(t))
			if tt.want == "" {
				if err == nil || selected != "" {
					t.Fatalf("unsupported mode accepted: %q %v", selected, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			r := s.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "input"}}, &recordingSink{})
			if selected != tt.want || r.Configuration.ActualMode != tt.want {
				t.Fatalf("selected=%q actual=%q", selected, r.Configuration.ActualMode)
			}
		})
	}
}
