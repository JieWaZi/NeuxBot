package acp

import (
	"context"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	"github.com/JieWaZi/NeuxBot/agent-runtime/driver/acp/native"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

// CodexConfig 保存 Codex 便捷入口的公开启动选项；所有原生 Agent 由共享工厂创建。
type CodexConfig struct {
	ID             string        // ID 是驱动身份，空值采用 codex。
	Logger         *slog.Logger  // Logger 是原生启动和诊断日志入口。
	CodexPath      string        // CodexPath 是明确选择的CLI，空值允许原生PATH查询。
	PrefixArgs     []string      // PrefixArgs 是宿主批准的原生启动前缀参数。
	Environment    []string      // Environment 是完整基础环境，nil表示启动时继承宿主环境。
	CleanupTimeout time.Duration // CleanupTimeout 是独立关闭期限。
}

// NewCodex 创建尚未启动 CLI 的 Codex Driver；不声明禁工具或显式配置隔离能力。
func NewCodex(config CodexConfig) (*Driver, error) {
	if config.Logger == nil {
		return nil, fault(contract.FaultInvalidSpec, "configure", "Codex driver requires a logger", nil)
	}
	if config.ID == "" {
		config.ID = "codex"
	}
	args := append([]string(nil), config.PrefixArgs...)
	var environment []string
	if config.Environment != nil {
		environment = append([]string{}, config.Environment...)
	}
	return New(Config{ID: config.ID, CleanupTimeout: config.CleanupTimeout, Factory: func(ctx context.Context, r contract.OpenRequest) (sdk.Agent, error) {
		base := environment
		if base == nil {
			base = os.Environ()
		}
		// 秘密只在此次创建时合并，Driver和会话不缓存凭据或完整环境。
		values := make(map[string]string, len(base)+len(r.Credential.Environment))
		for _, entry := range base {
			if key, value, ok := strings.Cut(entry, "="); ok {
				values[key] = value
			}
		}
		for key, value := range r.Credential.Environment {
			values[key] = value
		}
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		merged := make([]string, 0, len(keys))
		for _, key := range keys {
			merged = append(merged, key+"="+values[key])
		}
		agent, err := native.New(ctx, native.Config{RuntimeID: "codex", Logger: config.Logger, Executable: config.CodexPath, PrefixArgs: append([]string(nil), args...), Environment: merged})
		if err != nil {
			return nil, err
		}
		return agent, nil
	}})
}
