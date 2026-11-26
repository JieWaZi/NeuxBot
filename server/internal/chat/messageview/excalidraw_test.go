package messageview

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
)

func TestWorkBuddyExcalidrawSceneLiveAndReplay(t *testing.T) {
	builder := NewPublicMessageViewBuilder()
	builder.SeedNativeLiveMessage("reply", 1)
	input := map[string]any{
		"toolName": "mcp__plugin_excalidraw_excalidraw__create_view",
		"params": map[string]any{
			"elements": `[{"type":"cameraUpdate","width":800,"height":600},{"type":"rectangle","id":"dns","x":20,"y":30,"width":100,"height":40,"label":{"text":"DNS 服务"},"link":"https://example.invalid","secret":"PRIVATE"}]`,
			"ignored":  "PRIVATE",
		},
	}
	snapshot, _ := json.Marshal(map[string]any{"title": "DeferExecuteTool", "input": input, "status": "in_progress"})
	change := agent.MessageChange{ContentType: constants.RuntimeEventTypeToolUpdate, ToolCallID: "call", Data: snapshot, Revision: 2}
	before, err := builder.ApplyNativeIncrement("reply", change, 100)
	if err != nil {
		t.Fatal(err)
	}
	checkScene(t, before.Parts[0].Part, false, "")
	result := map[string]any{"status": "completed", "output": map[string]any{"text": `Diagram displayed! Checkpoint id: "08f8d6fac73847f1ac".`}}
	change.Data, _ = json.Marshal(result)
	change.Revision++
	after, err := builder.ApplyNativeIncrement("reply", change, 101)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"type":"rectangle","id":"dns","x":20,"y":30,"width":100,"height":40,"label":{"text":"DNS 服务"}}]`
	checkScene(t, after.Parts[0].Part, true, want)

	// 历史窗口读取当前工具快照，缺少前一条实时输入事件。
	result["title"], result["input"] = "DeferExecuteTool", input
	final, _ := json.Marshal(result)
	if err := builder.ReplaceStoredContent(agent.StoredMessage{ID: "reply", Parts: []agent.MessagePart{{
		Type: constants.RuntimeEventTypeToolUpdate, ToolCallID: "call", Data: final,
	}}}, 4, 102); err != nil {
		t.Fatal(err)
	}
	checkScene(t, &builder.PublicMessage("reply").DisplayParts[0], true, want)
}

func TestExcalidrawShowsUnavailableForIncompleteSceneOnly(t *testing.T) {
	for _, tc := range []struct {
		// name 标识应处理的工具调用场景。
		name string
		// data 是持久化原生工具快照。
		data string
		// present 表示回复下应当提供不可预览状态。
		present bool
	}{
		{"checkpoint without base", `{"status":"completed","title":"DeferExecuteTool","input":{"toolName":"mcp__plugin_excalidraw_excalidraw__create_view","params":{"elements":"[{\"type\":\"restoreCheckpoint\",\"id\":\"missing\"}]"}},"output":{"text":"Diagram displayed! Checkpoint id: \"abcd\"."}}`, true},
		{"no success receipt", `{"status":"completed","title":"DeferExecuteTool","input":{"toolName":"mcp__plugin_excalidraw_excalidraw__create_view","params":{"elements":"[{\"type\":\"rectangle\",\"id\":\"r\",\"x\":0,\"y\":0,\"width\":20,\"height\":20}]"}},"output":{"text":"Invalid JSON in elements"}}`, true},
		{"tool failed", `{"status":"failed","title":"mcp__excalidraw__create_view","input":{"elements":"[]"}}`, false},
		{"different tool", `{"status":"completed","title":"OtherTool","input":{"toolName":"mcp__plugin_excalidraw_excalidraw__create_view","params":{"elements":"[]"}}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			builder := NewPublicMessageViewBuilder()
			builder.SeedNativeLiveMessage("reply", 1)
			change := agent.MessageChange{ContentType: constants.RuntimeEventTypeToolUpdate, ToolCallID: "call", Data: json.RawMessage(tc.data), Revision: 2}
			update, err := builder.ApplyNativeIncrement("reply", change, 100)
			if err != nil {
				t.Fatal(err)
			}
			checkScene(t, update.Parts[0].Part, tc.present, "")
		})
	}
}

func TestExcalidrawPreservesExplicitZeroDimensions(t *testing.T) {
	scene := `[{"type":"line","id":"vertical","x":0,"y":0,"width":0,"height":60},{"type":"line","id":"horizontal","x":0,"y":0,"width":100,"height":0},{"type":"line","id":"default","x":0,"y":0}]`
	if got := normalizeExcalidrawElements(scene); got != scene {
		t.Fatalf("projected dimensions = %s, want %s", got, scene)
	}
}

func checkScene(t *testing.T, part *MessageDisplayPart, present bool, want string) {
	t.Helper()
	data, err := json.Marshal(part)
	if err != nil {
		t.Fatal(err)
	}
	var projected struct {
		// Excalidraw 是回复中图表的公开字段。
		Excalidraw *struct {
			// Elements 是可回放的图元 JSON。
			Elements string
		}
	}
	if err := json.Unmarshal(data, &projected); err != nil {
		t.Fatal(err)
	}
	if (projected.Excalidraw != nil) != present || present && projected.Excalidraw.Elements != want {
		t.Fatalf("diagram projection = %s, want present=%t elements=%s", data, present, want)
	}
	if strings.Contains(string(data), "PRIVATE") || strings.Contains(string(data), "example.invalid") {
		t.Fatalf("diagram projection exposed unrelated tool data: %s", data)
	}
}
