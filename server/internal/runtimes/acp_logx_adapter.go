package runtimes

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/google/uuid"
)

var (
	acpAdapterLoggerOnce   sync.Once
	sharedACPAdapterLogger *slog.Logger
)

// acpLogxHandler 是第三方 acp-go 强制 slog 接口到项目唯一 logx 输出的隔离适配器。
type acpLogxHandler struct {
	// attributes 保存 WithAttrs 预绑定的第三方诊断字段。
	attributes []slog.Attr
	// group 是 WithGroup 指定的字段前缀。
	group string
	// secrets 只保护当前Agent显式收到的插件授权秘密，不记录环境原文。
	secrets *strings.Replacer
	// stderr 聚合当前 Agent 被操作系统拆分的原生诊断行。
	stderr *acpStderrLines
}

// acpLogContextKey 只在 Runtime 内部传递日志身份，不进入模型输入或启动指纹。
type acpLogContextKey struct{}

// acpStderrLines 保存一条有界的未完成 stderr 行。
type acpStderrLines struct {
	// mutex 防止并发 stderr 写入与关闭刷尾交错。
	mutex sync.Mutex
	// pending 保存尚未收到换行的诊断片段。
	pending string
}

// acpAdapterLogger 返回当前 Agent 的诊断 Logger，SDK 默认日志也接入同一输出边界。
func acpAdapterLogger(ctx context.Context) *slog.Logger {
	acpAdapterLoggerOnce.Do(func() {
		sharedACPAdapterLogger = slog.New(&acpLogxHandler{})
		// 不在 SDK 启动读取 goroutine 后调用无同步保护的 SetLogger。
		slog.SetDefault(sharedACPAdapterLogger)
	})
	if handler, ok := ctx.Value(acpLogContextKey{}).(*acpLogxHandler); ok {
		return slog.New(handler)
	}
	return sharedACPAdapterLogger
}

// acpAgentLogContext 为每条连接绑定身份；可保留进程不绑定会过期的首轮 Invocation。
func acpAgentLogContext(ctx context.Context, config acpAdapterConfig, binding acpRuntimeBinding) context.Context {
	if _, ok := ctx.Value(acpLogContextKey{}).(*acpLogxHandler); ok {
		return ctx
	}
	operation := "invoke"
	if config.DetectionID != "" {
		operation = "detect"
	}
	handler := &acpLogxHandler{
		attributes: []slog.Attr{
			slog.String("runtime_id", config.RuntimeID),
			slog.String("base_runtime_id", binding.baseRuntimeID),
			slog.String("agent_id", uuid.NewString()),
			slog.String("operation", operation),
		},
		stderr: &acpStderrLines{},
	}
	pairs := []string{}
	for _, entry := range config.Environment {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || !strings.HasPrefix(key, constants.PluginAuthEnvironmentPrefix) || value == "" {
			continue
		}
		pairs = append(pairs, value, "[REDACTED]")
		if scheme, credential, ok := strings.Cut(value, " "); ok && strings.EqualFold(scheme, "Bearer") && strings.TrimSpace(credential) != "" {
			pairs = append(pairs, strings.TrimSpace(credential), "[REDACTED]")
		}
	}
	if len(pairs) > 0 {
		handler.secrets = strings.NewReplacer(pairs...)
	}
	if config.DetectionID != "" {
		handler.attributes = append(handler.attributes, slog.String("detection_id", config.DetectionID))
	}
	return context.WithValue(ctx, acpLogContextKey{}, handler)
}

// Enabled 保留第三方诊断，由 logx 自己执行最终日志等级过滤。
func (*acpLogxHandler) Enabled(context.Context, slog.Level) bool { return true }

// Handle 把第三方结构字段格式化后交给 logx，项目业务代码不接触 slog。
func (h *acpLogxHandler) Handle(_ context.Context, record slog.Record) error {
	if record.Message == "native ACP stderr" && h.stderr != nil {
		record.Attrs(func(attribute slog.Attr) bool {
			if attribute.Key == "stderr" {
				for _, line := range h.stderr.append(attribute.Value.String(), false) {
					h.writeNativeLine(line)
				}
			}
			return true
		})
		return nil
	}
	if record.Message == "connection closed" && record.Level <= slog.LevelInfo {
		// SDK 在正常 EOF、主动关闭 Pipe 和真实失败时共用这条消息。
		// 常见收尾只留 Debug；真实执行失败仍由调用边界记录带身份的 Error。
		record.Level = slog.LevelWarn
		record.Attrs(func(attribute slog.Attr) bool {
			if attribute.Key == "cause" {
				switch attribute.Value.String() {
				case "connection closed", "peer connection closed", "io: read/write on closed pipe", "read |0: file already closed":
					record.Level = slog.LevelDebug
				}
			}
			return true
		})
	}
	h.write(record)
	return nil
}

// write 将统一等级和字段交给 logx，避免 SDK 另行写 stderr。
func (h *acpLogxHandler) write(record slog.Record) {
	fields := make([]string, 0, len(h.attributes)+record.NumAttrs())
	for _, attribute := range h.attributes {
		fields = append(fields, h.formatAttribute(attribute))
	}
	record.Attrs(func(attribute slog.Attr) bool {
		fields = append(fields, h.formatAttribute(attribute))
		return true
	})
	message := h.redact(record.Message)
	if len(fields) > 0 {
		message += ": " + strings.Join(fields, " ")
	}
	switch {
	case record.Level >= slog.LevelError:
		logx.Error(message)
	case record.Level >= slog.LevelWarn:
		logx.Warn(message)
	case record.Level <= slog.LevelDebug:
		logx.Debug(message)
	default:
		logx.Info(message)
	}
}

// append 按行重组任意写入分块，超长无换行内容分段输出而不无限占用内存。
func (s *acpStderrLines) append(fragment string, flush bool) []string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.pending += fragment
	var lines []string
	const maxLineBytes = 64 << 10
	for len(s.pending) > 0 {
		index := strings.IndexByte(s.pending, '\n')
		if index >= 0 && index <= maxLineBytes {
			lines = append(lines, s.pending[:index])
			s.pending = s.pending[index+1:]
		} else if len(s.pending) >= maxLineBytes {
			lines = append(lines, s.pending[:maxLineBytes])
			s.pending = s.pending[maxLineBytes:]
		} else {
			break
		}
	}
	if flush && s.pending != "" {
		lines = append(lines, s.pending)
		s.pending = ""
	}
	return lines
}

// flushACPStderr 在进程资源回收后保留最后一条没有换行的原生诊断。
func flushACPStderr(ctx context.Context) {
	if h, ok := ctx.Value(acpLogContextKey{}).(*acpLogxHandler); ok {
		for _, line := range h.stderr.append("", true) {
			h.writeNativeLine(line)
		}
	}
}

// writeNativeLine 保留原始诊断内容，JSON 和明确文本前缀决定级别。
func (h *acpLogxHandler) writeNativeLine(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	level := acpNativeStderrLevel(line)
	record := slog.Record{Message: "native ACP stderr", Level: level}
	record.AddAttrs(slog.String("stderr", line))
	h.write(record)
}

// acpNativeStderrLevel 对未知内容保留警告，避免把没有级别的故障静默降级。
func acpNativeStderrLevel(line string) slog.Level {
	var object map[string]json.RawMessage
	if json.Unmarshal([]byte(line), &object) == nil && object != nil {
		var value string
		_ = json.Unmarshal(object["level"], &value)
		if value == "" {
			_ = json.Unmarshal(object["severity"], &value)
		}
		level, ok := acpNativeLogLevel(value)
		if ok {
			// readiness fallback 等 info 日志若携带真实错误，仍保留警告。
			failure := strings.TrimSpace(string(object["error"]))
			if failure != "" && failure != "null" && failure != `""` && level < slog.LevelWarn {
				return slog.LevelWarn
			}
			return level
		}
	}
	// 原生 CLI 仅跳过未完成阶段的性能统计，不表示模型调用失败。
	if strings.HasPrefix(line, "[STARTUP] Phase '") && strings.HasSuffix(line, "was started but never ended. Skipping metrics.") {
		return slog.LevelDebug
	}
	fields := strings.Fields(line)
	if len(fields) > 0 {
		if level, ok := acpNativeLogLevel(strings.Trim(fields[0], "[]:")); ok {
			return level
		}
	}
	// 标准库 slog 文本带日期时间，其他 CLI 也可能使用 level=info 格式。
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		// 只读 logfmt 的头字段，正文或异常文本中的 level= 不能降低诊断级别。
		if !ok || key == "msg" || key == "message" {
			break
		}
		if key == "level" {
			if level, known := acpNativeLogLevel(strings.Trim(value, `"`)); known {
				return level
			}
		}
	}
	if len(fields) > 2 && len(fields[0]) == len("2006/01/02") && len(fields[1]) == len("15:04:05") {
		if level, ok := acpNativeLogLevel(fields[2]); ok {
			return level
		}
	}
	return slog.LevelWarn
}

// acpNativeLogLevel 只接纳原生诊断明确声明的常见日志等级。
func acpNativeLogLevel(value string) (slog.Level, bool) {
	switch strings.ToLower(value) {
	case "trace", "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error", "fatal", "panic":
		return slog.LevelError, true
	default:
		return slog.LevelWarn, false
	}
}

// WithAttrs 返回保留已有字段的不可变 Handler 副本。
func (h *acpLogxHandler) WithAttrs(attributes []slog.Attr) slog.Handler {
	clone := &acpLogxHandler{
		attributes: append([]slog.Attr(nil), h.attributes...),
		group:      h.group,
		stderr:     h.stderr,
		secrets:    h.secrets,
	}
	clone.attributes = append(clone.attributes, attributes...)
	return clone
}

// WithGroup 返回为后续字段增加组名前缀的 Handler 副本。
func (h *acpLogxHandler) WithGroup(group string) slog.Handler {
	clone := &acpLogxHandler{
		attributes: append([]slog.Attr(nil), h.attributes...),
		group:      strings.Trim(strings.TrimSpace(h.group+"."+group), "."),
		stderr:     h.stderr,
		secrets:    h.secrets,
	}
	return clone
}

// formatAttribute 把第三方属性转换为不含协议原文的键值诊断。
func (h *acpLogxHandler) formatAttribute(attribute slog.Attr) string {
	key := attribute.Key
	if h.group != "" {
		key = h.group + "." + key
	}
	return key + "=" + fmt.Sprintf("%q", h.redact(fmt.Sprint(attribute.Value.Any())))
}

// redact 在日志格式化前去掉当前进程的插件秘密，兼容结构属性和原生stderr。
func (h *acpLogxHandler) redact(value string) string {
	if h.secrets == nil {
		return value
	}
	return h.secrets.Replace(value)
}
