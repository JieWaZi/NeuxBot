package messageview

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

// TestNativeToolDisplayUsesACPFields 验证原生 ACP 工具按操作和命令展示，排除 cwd 与元数据。
func TestNativeToolDisplayUsesACPFields(t *testing.T) {
	tests := []struct {
		// name 标识原生工具事件的协议形态。
		name string
		// data 是经过 AgentTool 存储的 ACP 工具快照。
		data string
		// label 是用户应看到的操作或命令。
		label string
		// action 是操作摘要的业务语义。
		action constants.MessageToolAction
		// path 是明确的文件目标；命令工作目录不构成目标。
		path string
	}{
		{"codex command title", `{"kind":"execute","title":"/bin/zsh -lc 'ls -la'","input":null}`, "执行 /bin/zsh -lc 'ls -la'", constants.MessageToolActionExecuteCommand, ""},
		{"codex read command keeps command", `{"kind":"execute","title":"/bin/zsh -lc 'cat SKILL.md'","input":null}`, "读取文件 · /bin/zsh -lc 'cat SKILL.md'", constants.MessageToolActionReadFile, ""},
		{"codex tests", `{"kind":"execute","title":"go test ./internal/chat/messageview","input":null}`, "运行测试 · go test ./internal/chat/messageview", constants.MessageToolActionRunTests, ""},
		{"command mentioning MCP is still a command", `{"kind":"execute","title":"/bin/zsh -lc 'cat docs/mcp.md'","input":null}`, "读取文件 · /bin/zsh -lc 'cat docs/mcp.md'", constants.MessageToolActionReadFile, ""},
		{"claude bash title", `{"kind":"execute","title":"Bash","input":{"command":"ls -la","description":"查看目录"}}`, "查看目录 · ls -la", constants.MessageToolActionExecuteCommand, ""},
		{"claude read title", `{"kind":"read","title":"Read","input":{"file_path":"docs/README.md"}}`, "读取 README.md", constants.MessageToolActionReadFile, "docs/README.md"},
		{"ACP image read title", `{"kind":"read","title":"View Image chart.png","input":{"path":"chart.png"}}`, "查看图片 chart.png", constants.MessageToolActionViewImage, "chart.png"},
		{"claude write title", `{"kind":"edit","title":"Write","input":{"file_path":"docs/README.md","content":"PRIVATE_BODY"}}`, "写入 README.md", constants.MessageToolActionWriteFile, "docs/README.md"},
		{"claude search title", `{"kind":"search","title":"Grep","input":{"pattern":"adapter","glob":"**/*.go"}}`, "搜索内容 /adapter/ in **/*.go", constants.MessageToolActionSearchFiles, ""},
		{"pi bash without kind", `{"title":"bash","input":{"command":"pwd"}}`, "执行 pwd", constants.MessageToolActionExecuteCommand, ""},
		{"pi read without kind", `{"title":"read","input":{"path":"main.go"}}`, "读取 main.go", constants.MessageToolActionReadFile, "main.go"},
		{"standard ACP execute with descriptive title", `{"kind":"execute","title":"Run project command","input":{"command":"npm run dev"}}`, "执行 npm run dev", constants.MessageToolActionExecuteCommand, ""},
		{"standard ACP cmd argument", `{"kind":"execute","title":"Shell","input":{"cmd":"git status --short"}}`, "检查 Git 变更 · git status --short", constants.MessageToolActionInspectGit, ""},
		{"standard ACP file kind", `{"kind":"read","title":"Read project settings","input":{"path":"config.json"}}`, "读取 config.json", constants.MessageToolActionReadFile, "config.json"},
		{"standard ACP native title fallback", `{"kind":"other","title":"Inspect project dependencies","input":null}`, "Inspect project dependencies", constants.MessageToolActionOther, ""},
		{"standard ACP read title without parameters", `{"kind":"read","title":"Read project settings","input":null}`, "Read project settings", constants.MessageToolActionReadFile, ""},
		{"standard ACP edit title without parameters", `{"kind":"edit","title":"Update project settings","input":null}`, "Update project settings", constants.MessageToolActionModifyFile, ""},
		{"standard ACP web search", `{"kind":"fetch","title":"WebSearch","input":{"query":"ACP protocol"}}`, "搜索网页 \"ACP protocol\"", constants.MessageToolActionSearchWeb, ""},
		{"codex web search title", `{"kind":"search","title":"webSearch","input":null}`, "搜索网页", constants.MessageToolActionSearchWeb, ""},
		{"MCP title identity", `{"kind":"other","title":"mcp__bot_memory_builtin__memory_search","input":{"query":"用户偏好"}}`, "搜索记忆", constants.MessageToolActionCallService, ""},
		{"cursor nested MCP input", `{"kind":"other","title":"bot_memory_builtin: memory_search","input":{"providerIdentifier":"bot_memory_builtin","toolName":"memory_search","args":{"query":"用户偏好"}}}`, "搜索记忆", constants.MessageToolActionCallService, ""},
		{"unknown tool without title", `{"kind":"other","input":null}`, "执行操作", constants.MessageToolActionOther, ""},
		{"custom title ignores metadata", `{"kind":"other","title":"Refresh dependencies","id":"native-tool-id","input":{}}`, "Refresh dependencies", constants.MessageToolActionOther, ""},
		{"human title mentioning MCP is not a tool identity", `{"kind":"other","title":"Inspect MCP configuration","input":null}`, "Inspect MCP configuration", constants.MessageToolActionOther, ""},
		{"kimi flattened builtin title", `{"kind":"other","title":"bot_memory_builtin_memory_search","input":{"query":"用户偏好"}}`, "搜索记忆", constants.MessageToolActionCallService, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var data map[string]any
			if err := json.Unmarshal([]byte(tt.data), &data); err != nil {
				t.Fatal(err)
			}
			// 除明确输入路径外，所有事件都带同一个执行 cwd，不能被显示为文件目标。
			data["locations"] = []any{map[string]any{"path": "/workspace/Agents/223553a516a0baff0fd7bdcfbc057273"}}
			encoded, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			builder := NewPublicMessageViewBuilder()
			for _, record := range []eventlog.Record{chatCreatedRecord(1, "/workspace/Agents/223553a516a0baff0fd7bdcfbc057273"), executionCreatedRecord(2, "execution")} {
				if _, err := builder.Apply(record); err != nil {
					t.Fatal(err)
				}
			}
			part := agent.MessagePart{Type: constants.RuntimeEventTypeToolUpdate, ToolCallID: "native-tool-id", Data: encoded}
			if err := builder.ReplaceStoredContent(agent.StoredMessage{ID: "execution", Parts: []agent.MessagePart{part}}, 3, 100); err != nil {
				t.Fatal(err)
			}
			got := builder.PublicMessage("execution").DisplayParts[0]
			if got.ToolLabel != tt.label || got.ToolLoadingLabel != "正在"+tt.label+"…" || got.ToolAction != tt.action {
				t.Fatalf("native display = %+v, want label %q and action %s", got, tt.label, tt.action)
			}
			if tt.path == "" && got.ToolSafeSummary != nil && got.ToolSafeSummary.Path != "" || tt.path != "" && (got.ToolSafeSummary == nil || got.ToolSafeSummary.Path != tt.path) {
				t.Fatalf("target = %+v, want path %q", got.ToolSafeSummary, tt.path)
			}
			public, _ := json.Marshal(got)
			for _, private := range []string{"223553a516a0baff0fd7bdcfbc057273", "PRIVATE_BODY"} {
				if strings.Contains(string(public), private) {
					t.Fatalf("native display exposes non-target data: %s", public)
				}
			}
		})
	}
}

// TestNativeToolDisplayClearsCopiedCommandAliases 验证清空输入同时清除复制命令的语义。
func TestNativeToolDisplayClearsCopiedCommandAliases(t *testing.T) {
	builder := NewPublicMessageViewBuilder()
	builder.SeedNativeLiveMessage("execution", 1)
	for i, data := range []string{
		`{"kind":"execute","title":"Bash","input":{"command":"go test ./example"},"command":"go test ./example"}`,
		`{"input":null}`,
	} {
		if _, err := builder.ApplyNativeIncrement("execution", agent.MessageChange{ContentType: constants.RuntimeEventTypeToolUpdate, ToolCallID: "call", Data: json.RawMessage(data), Revision: int64(i + 2)}, 100); err != nil {
			t.Fatal(err)
		}
	}
	part := builder.PublicMessage("execution").DisplayParts[0]
	if part.ToolAction != constants.MessageToolActionExecuteCommand || part.ToolLabel != "执行命令" {
		t.Fatalf("cleared input kept command alias or inferred action: %+v", part)
	}
}

// TestNativeToolDisplayRecomputesLiveCommandAndReplaysSnapshot 验证实时更新与最终快照回放一致。
func TestNativeToolDisplayRecomputesLiveCommandAndReplaysSnapshot(t *testing.T) {
	builder := NewPublicMessageViewBuilder()
	for _, record := range []eventlog.Record{chatCreatedRecord(1, "/workspace/project"), executionCreatedRecord(2, "execution")} {
		if _, err := builder.Apply(record); err != nil {
			t.Fatal(err)
		}
	}
	steps := []struct {
		// data 表达输入替换、字段省略或原生终态。
		data string
		// label 是这一步的最新操作摘要。
		label string
	}{
		{`{"kind":"execute","title":"Bash","input":{"command":"ls -la"},"locations":[{"path":"/workspace/project"}]}`, "执行 ls -la"},
		{`{"input":{"command":"git status --short"}}`, "检查 Git 变更 · git status --short"},
		{`{"input":null}`, "执行命令"},
		{`{"title":"go test ./internal/chat/messageview"}`, "运行测试 · go test ./internal/chat/messageview"},
		{`{"status":"completed","content":[]}`, "运行测试 · go test ./internal/chat/messageview"},
	}
	var live MessageDisplayPart
	for i, step := range steps {
		increment, err := builder.ApplyNativeIncrement("execution", agent.MessageChange{
			ContentType: constants.RuntimeEventTypeToolUpdate, ToolCallID: "call", Data: json.RawMessage(step.data), Revision: int64(i + 3),
		}, 100)
		if err != nil {
			t.Fatal(err)
		}
		live = builder.PublicMessage("execution").DisplayParts[0]
		if live.ToolLabel != step.label || live.ToolSafeSummary != nil || len(increment.Parts) != 1 || increment.Parts[0].Part.ToolLabel != step.label {
			t.Fatalf("step %d display = %+v, increment = %+v, want %q", i, live, increment, step.label)
		}
	}
	if live.ToolStatus != constants.MessageToolStatusCompleted {
		t.Fatalf("tool did not complete: %+v", live)
	}
	final := agent.MessagePart{Type: constants.RuntimeEventTypeToolUpdate, ToolCallID: "call", Data: json.RawMessage(`{"kind":"execute","title":"go test ./internal/chat/messageview","input":null,"locations":[{"path":"/workspace/project"}],"status":"completed","content":[]}`)}
	if err := builder.ReplaceStoredContent(agent.StoredMessage{ID: "execution", Parts: []agent.MessagePart{final}}, 7, 100); err != nil {
		t.Fatal(err)
	}
	replayed := builder.PublicMessage("execution").DisplayParts[0]
	if !reflect.DeepEqual(live, replayed) {
		t.Fatalf("live and replay differ: live=%+v replay=%+v", live, replayed)
	}
}

// TestNativeFileLocationsSupplyAndClearTargets 验证文件操作沿用或清除原生位置目标。
func TestNativeFileLocationsSupplyAndClearTargets(t *testing.T) {
	for _, title := range []string{"Read", "list_files"} {
		t.Run(title, func(t *testing.T) {
			builder := NewPublicMessageViewBuilder()
			for _, record := range []eventlog.Record{chatCreatedRecord(1, "/workspace"), executionCreatedRecord(2, "execution")} {
				if _, err := builder.Apply(record); err != nil {
					t.Fatal(err)
				}
			}
			initial, _ := json.Marshal(map[string]any{"title": title, "input": nil, "locations": []any{map[string]any{"path": "/workspace/docs/README.md"}}})
			for i, data := range []string{
				string(initial),
				`{"locations":[]}`,
			} {
				if _, err := builder.ApplyNativeIncrement("execution", agent.MessageChange{ContentType: constants.RuntimeEventTypeToolUpdate, ToolCallID: "call", Data: json.RawMessage(data), Revision: int64(i + 2)}, 100); err != nil {
					t.Fatal(err)
				}
				part := builder.PublicMessage("execution").DisplayParts[0]
				want := "读取 README.md"
				if i == 1 {
					want = "读取文件"
				}
				if title == "list_files" {
					want = "浏览 README.md"
					if i == 1 {
						want = "浏览文件"
					}
				}
				if part.ToolLabel != want {
					t.Fatalf("step %d display = %+v, want %q", i, part, want)
				}
				if i == 0 && (part.ToolSafeSummary == nil || part.ToolSafeSummary.Path != "docs/README.md") || i == 1 && part.ToolSafeSummary != nil {
					t.Fatalf("step %d file target = %+v", i, part.ToolSafeSummary)
				}
			}
		})
	}
}

func TestRunDurationIncludesRequestPreparationAfterReplay(t *testing.T) {
	for _, data := range []string{`{"requestedAt":100}`, `{}`, `{"requestedAt":600}`} {
		builder := NewPublicMessageViewBuilder()
		for _, record := range []eventlog.Record{
			{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "turn", Sequence: 1, RecordedAt: 500, Data: json.RawMessage(data)},
			{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: "turn", Sequence: 2, RecordedAt: 500},
			{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "turn", Sequence: 3, RecordedAt: 900, Data: json.RawMessage(`{"status":"completed"}`)},
		} {
			if _, err := builder.Apply(record); err != nil {
				t.Fatal(err)
			}
		}
		metrics := builder.PublicMessage("turn").RunMetrics
		wantStart, wantDuration := int64(500), int64(400)
		if data == `{"requestedAt":100}` {
			wantStart, wantDuration = 100, 800
		}
		if metrics == nil || metrics.StartedAt != wantStart || metrics.DurationMs != wantDuration {
			t.Fatalf("data=%s metrics=%+v, want start=%d duration=%d", data, metrics, wantStart, wantDuration)
		}
	}
}

func TestNativeUsagePublicMetricsRequireCompleteCoverage(t *testing.T) {
	const id = "execution"
	builder := NewPublicMessageViewBuilder()
	records := []eventlog.Record{
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: id, Sequence: 1, RecordedAt: 90, Data: json.RawMessage(`{}`)},
		{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: id, Sequence: 2, RecordedAt: 100},
		{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: id, Sequence: 3, RecordedAt: 250, Data: json.RawMessage(`{"status":"completed"}`)},
	}
	for _, record := range records {
		if _, err := builder.Apply(record); err != nil {
			t.Fatal(err)
		}
	}
	input, output, cache, knownZero := int64(81), int64(17), int64(8), int64(0)
	partial := &acpmeta.TurnUsage{Coverage: "last_request", InputTokens: &input, OutputTokens: &output}
	complete := &acpmeta.TurnUsage{Coverage: "invocation", Complete: true, InputTokens: &input, CacheReadTokens: &cache, ReasoningTokens: &knownZero}
	wantTime := map[string]int64{"startedAt": 100, "durationMs": 150}
	check := func(usage *acpmeta.TurnUsage, want map[string]int64) {
		t.Helper()
		builder.SetNativeUsage(id, usage)
		raw, err := json.Marshal(builder.PublicMessage(id))
		if err != nil {
			t.Fatal(err)
		}
		var public struct {
			// RunMetrics is the existing public whole-turn projection.
			RunMetrics map[string]int64 `json:"runMetrics"`
		}
		if err := json.Unmarshal(raw, &public); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(public.RunMetrics, want) {
			t.Fatalf("serialized metrics = %v, want %v", public.RunMetrics, want)
		}
	}
	check(partial, wantTime)
	check(complete, map[string]int64{"startedAt": 100, "durationMs": 150, "inputTokens": 81, "cacheReadTokens": 8})
	check(partial, wantTime)
	check(complete, map[string]int64{"startedAt": 100, "durationMs": 150, "inputTokens": 81, "cacheReadTokens": 8})
	check(nil, wantTime)
}

func TestNativePartialUsageIncludesKnownSubtotal(t *testing.T) {
	const id = "execution"
	builder := NewPublicMessageViewBuilder()
	_, err := builder.Apply(eventlog.Record{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: id,
		Sequence: 1, RecordedAt: 90, Data: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	input, output, reasoning := int64(5), int64(3), int64(1)
	result := &agent.NativeResult{Key: agent.NativeResultKey{Session: agent.NativeSessionKey{Provider: "codex", Subject: "s", SessionID: "n"}, InvocationID: "p"},
		Turn: acpmeta.NativeTurn{Usage: &acpmeta.TurnUsage{Coverage: "last_request", InputTokens: &input, OutputTokens: &output, ReasoningTokens: &reasoning}}}
	builder.SetNativeResult(id, result)
	usage := builder.PublicMessage(id).RunMetrics.TokenUsage
	if usage == nil || usage.InvocationID == "" || usage.Complete || usage.TotalWithCacheRead != nil ||
		usage.KnownTotalWithCacheRead == nil || *usage.KnownTotalWithCacheRead != 8 || usage.IncompleteUsageInvocations != 1 {
		t.Fatalf("unexpected public partial metrics: %+v", usage)
	}
}

func TestNativeReportedInputIsVisibleWithoutInventingUncachedInput(t *testing.T) {
	builder := NewPublicMessageViewBuilder()
	builder.SeedNativeLiveMessage("reported-input", 1)
	input, output := int64(1200), int64(20)
	builder.SetNativeResult("reported-input", &agent.NativeResult{
		Key: agent.NativeResultKey{Session: agent.NativeSessionKey{Provider: "qoder", Subject: "s", SessionID: "n"}, InvocationID: "p"},
		Turn: acpmeta.NativeTurn{Usage: &acpmeta.TurnUsage{Source: "qoder.history.usage", OutputTokens: &output,
			Observations: []acpmeta.UsageObservation{{Source: "qoder.history.usage", Coverage: "observed_requests_raw", InputTotalTokens: &input}}}},
	})
	raw, err := json.Marshal(builder.PublicMessage("reported-input").RunMetrics.TokenUsage.KnownTokens)
	if err != nil {
		t.Fatal(err)
	}
	var known map[string]*int64
	if err := json.Unmarshal(raw, &known); err != nil {
		t.Fatal(err)
	}
	if known["reportedInput"] == nil || *known["reportedInput"] != 1200 || known["input"] != nil {
		t.Fatalf("reported input missing or invented as uncached input: %s", raw)
	}
}

func TestNativeResultShowsQoderSubtotalAndWorkBuddySampleSeparately(t *testing.T) {
	const id = "execution"
	builder := NewPublicMessageViewBuilder()
	builder.SeedNativeLiveMessage(id, 1)
	output := int64(31)
	result := &agent.NativeResult{
		Key:  agent.NativeResultKey{Session: agent.NativeSessionKey{Provider: "qoder", Subject: "s", SessionID: "n"}, InvocationID: "p"},
		Turn: acpmeta.NativeTurn{Usage: &acpmeta.TurnUsage{Coverage: "query_pipeline", OutputTokens: &output}},
	}
	builder.SetNativeResult(id, result)
	metrics := builder.PublicMessage(id).RunMetrics
	if metrics == nil || metrics.TokenUsage == nil || metrics.TokenUsage.KnownTokens.Output == nil ||
		*metrics.TokenUsage.KnownTokens.Output != output || metrics.TokenUsage.Complete || metrics.OutputTokens != 0 {
		t.Fatalf("Qoder should expose the known subtotal: %+v", metrics)
	}
	result.Key.Session.Provider = "workbuddy"
	result.Turn.Usage = &acpmeta.TurnUsage{Observations: []acpmeta.UsageObservation{{
		Source: "codebuddy/usage_update", Coverage: "latest_model_request", Tokens: acpmeta.ModelUsage{OutputTokens: &output},
	}}}
	builder.SetNativeResult(id, result)
	metrics = builder.PublicMessage(id).RunMetrics
	if metrics == nil || metrics.LatestRequestOutputTokens == nil || *metrics.LatestRequestOutputTokens != output ||
		metrics.TokenUsage != nil || metrics.OutputTokens != 0 {
		t.Fatalf("WorkBuddy should expose the sample separately: %+v", metrics)
	}
}

func TestNativeResultPreservesExplicitFailureSource(t *testing.T) {
	result := &agent.NativeResult{Failure: &acpmeta.NativeTurnFailure{Message: "Native quota exhausted"}}
	for _, source := range []constants.ErrorMessageSource{constants.ErrorMessageSourceUnspecified, constants.ErrorMessageSourceLocalized, constants.ErrorMessageSourcePublic} {
		builder := NewPublicMessageViewBuilder()
		for _, record := range []eventlog.Record{
			{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "execution", Sequence: 1, Data: json.RawMessage(`{}`)},
			{Type: constants.ChatEventTypeExecutionFailed, ExecutionID: "execution", Sequence: 2, Data: json.RawMessage(`{"status":"failed","errorCode":"RuntimeQuotaExhausted","errorMessage":"Native quota exhausted","errorMessageSource":"` + string(source) + `"}`)},
		} {
			if _, err := builder.Apply(record); err != nil {
				t.Fatal(err)
			}
		}
		builder.SetNativeResult("execution", result)
		message := builder.PublicMessage("execution")
		if message == nil || message.ErrorMessageSource != source || message.ErrorMessage != "Native quota exhausted" {
			t.Fatalf("source=%q projection=%+v", source, message)
		}
	}
}
