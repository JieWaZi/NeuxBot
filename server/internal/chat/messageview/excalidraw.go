package messageview

import (
	"encoding/json"
	"math"
	"regexp"
	"strings"
)

const (
	excalidrawTool           = "create_view"
	excalidrawServer         = "excalidraw"
	excalidrawPluginServer   = "plugin_excalidraw_excalidraw"
	maxExcalidrawBytes       = 64 * 1024
	maxExcalidrawElements    = 300
	maxExcalidrawCoordinate  = 16000
	maxExcalidrawPatternSpan = 100000
)

var (
	excalidrawColor = regexp.MustCompile(`^(#[0-9a-fA-F]{3,4}|#[0-9a-fA-F]{6}|#[0-9a-fA-F]{8}|transparent)?$`)
	excalidrawID    = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,80}$`)
)

// MessageExcalidraw 是插件绘图成果。
type MessageExcalidraw struct {
	// 图元数据
	Elements string `json:"elements,omitempty"`
}

// excalidrawElement 是绘图工具可在本地预览的图元白名单。
type excalidrawElement struct {
	// Type 是图元形状或画布指令。
	Type string `json:"type"`
	// ID 是场景内稳定的图元标识。
	ID string `json:"id"`
	// X 是图元起点的横坐标。
	X float64 `json:"x"`
	// Y 是图元起点的纵坐标。
	Y float64 `json:"y"`
	// Width 是图元的水平跨度，保留垂直线显式填写的零值。
	Width *float64 `json:"width,omitempty"`
	// Height 是图元的垂直跨度，保留水平线显式填写的零值。
	Height *float64 `json:"height,omitempty"`
	// Angle 是图元绕中心的旋转角度。
	Angle float64 `json:"angle,omitempty"`
	// StrokeColor 是轮廓或文字颜色。
	StrokeColor string `json:"strokeColor,omitempty"`
	// BackgroundColor 是填充颜色。
	BackgroundColor string `json:"backgroundColor,omitempty"`
	// FillStyle 是形状的填充纹理。
	FillStyle string `json:"fillStyle,omitempty"`
	// StrokeStyle 是线条的绘制样式。
	StrokeStyle string `json:"strokeStyle,omitempty"`
	// StrokeWidth 是线条宽度。
	StrokeWidth *float64 `json:"strokeWidth,omitempty"`
	// Roughness 是手绘线条的抖动幅度。
	Roughness *float64 `json:"roughness,omitempty"`
	// Opacity 是图元的不透明度百分比。
	Opacity *float64 `json:"opacity,omitempty"`
	// Roundness 是形状拐角的圆角设置。
	Roundness *excalidrawRoundness `json:"roundness,omitempty"`
	// Text 是独立文本图元的正文。
	Text string `json:"text,omitempty"`
	// FontSize 是独立文本图元的字号。
	FontSize float64 `json:"fontSize,omitempty"`
	// TextAlign 是独立文本的水平对齐。
	TextAlign string `json:"textAlign,omitempty"`
	// Label 是绑定到图形的文本。
	Label *excalidrawLabel `json:"label,omitempty"`
	// Points 是折线各点相对起点的坐标。
	Points [][]float64 `json:"points,omitempty"`
	// StartArrowhead 是起点箭头类型；null 显式关闭箭头。
	StartArrowhead json.RawMessage `json:"startArrowhead,omitempty"`
	// EndArrowhead 是终点箭头类型；null 显式关闭箭头。
	EndArrowhead json.RawMessage `json:"endArrowhead,omitempty"`
	// StartBinding 是起点连接的形状。
	StartBinding *excalidrawBinding `json:"startBinding,omitempty"`
	// EndBinding 是终点连接的形状。
	EndBinding *excalidrawBinding `json:"endBinding,omitempty"`
	// DeleteIDs 是删除指令引用的逗号分隔标识，投影前消费。
	DeleteIDs string `json:"ids,omitempty"`
}

type excalidrawRoundness struct {
	// Type 是圆角的算法类型。
	Type int `json:"type"`
	// Value 是显式圆角半径。
	Value float64 `json:"value,omitempty"`
}

type excalidrawLabel struct {
	// Text 是绑定文本正文。
	Text string `json:"text"`
	// FontSize 是绑定文本字号。
	FontSize float64 `json:"fontSize,omitempty"`
	// StrokeColor 是绑定文本颜色。
	StrokeColor string `json:"strokeColor,omitempty"`
	// TextAlign 是绑定文本的水平对齐。
	TextAlign string `json:"textAlign,omitempty"`
	// VerticalAlign 是绑定文本的垂直对齐。
	VerticalAlign string `json:"verticalAlign,omitempty"`
}

type excalidrawBinding struct {
	// ElementID 是连接目标的图元标识。
	ElementID string `json:"elementId"`
	// FixedPoint 是目标形状内的相对连接位置。
	FixedPoint []float64 `json:"fixedPoint,omitempty"`
}

// projectExcalidrawInput 只保留已识别绘图工具的有界图元，沿用普通工具输入的替换语义。
func (s *toolProjectionState) projectExcalidrawInput(data map[string]any) {
	name := displayToolName(s.fields)
	deferred := firstDisplayString(s.fields, "title", "name") == "DeferExecuteTool"
	input, present := data["rawInput"]
	if !present {
		input, present = data["input"]
	}
	identityData := s.fields
	server, tool := mcpServiceAndTool(identityData, name)
	isDrawingTool := tool == excalidrawTool && (server == excalidrawServer || server == excalidrawPluginServer)
	if deferred {
		current := nestedObject(identityData, "rawInput")
		if _, rawPresent := identityData["rawInput"]; !rawPresent {
			current = nestedObject(identityData, "input")
		}
		innerServer, innerTool := mcpServiceAndTool(nil, firstDisplayString(current, "toolName"))
		isDrawingTool = innerTool == excalidrawTool && (innerServer == excalidrawServer || innerServer == excalidrawPluginServer)
	}
	if !isDrawingTool {
		s.excalidraw = nil
		return
	}
	if !present && s.excalidraw != nil {
		return
	}
	args, _ := input.(map[string]any)
	if deferred {
		args = nestedObject(args, "params")
	} else if args["elements"] == nil {
		args = nestedObject(args, "arguments")
	}
	elements, _ := args["elements"].(string)
	s.excalidraw = &MessageExcalidraw{Elements: normalizeExcalidrawElements(elements)}
}

func normalizeExcalidrawElements(encoded string) string {
	if len(encoded) == 0 || len(encoded) > maxExcalidrawBytes {
		return ""
	}
	var elements []excalidrawElement
	if json.Unmarshal([]byte(encoded), &elements) != nil || len(elements) > maxExcalidrawElements {
		return ""
	}
	drawn := make([]excalidrawElement, 0, len(elements))
	deleted, ids := make(map[string]bool), make(map[string]bool)
	var patternSpan float64
	for _, el := range elements {
		switch el.Type {
		case "cameraUpdate":
			continue
		case "delete":
			for _, id := range strings.Split(el.DeleteIDs, ",") {
				deleted[strings.TrimSpace(id)] = true
			}
			continue
		case "rectangle", "ellipse", "diamond", "arrow", "line", "text":
			if ids[el.ID] || !validExcalidrawElement(el) {
				return ""
			}
			el.DeleteIDs = ""
			ids[el.ID] = true
			drawn = append(drawn, el)
		default:
			// Checkpoint 续画可能依赖旧场景；孤立回放不会假装它是完整图表。
			return ""
		}
	}
	result := drawn[:0]
	for _, el := range drawn {
		if deleted[el.ID] {
			continue
		}
		// 纹理填充按几何跨度扩展线段，单看输入字节数无法限定预览开销。
		if el.BackgroundColor != "" && el.BackgroundColor != "transparent" && el.FillStyle != "solid" {
			patternSpan += math.Abs(excalidrawDimension(el.Width)) + math.Abs(excalidrawDimension(el.Height))
			if patternSpan > maxExcalidrawPatternSpan {
				return ""
			}
		}
		result = append(result, el)
	}
	if len(result) == 0 {
		return ""
	}
	safe, err := json.Marshal(result)
	if err != nil || len(safe) > maxExcalidrawBytes {
		return ""
	}
	return string(safe)
}

func validExcalidrawElement(el excalidrawElement) bool {
	if !excalidrawID.MatchString(el.ID) || el.ID == "__proto__" || el.ID == "constructor" || el.ID == "prototype" ||
		!validExcalidrawCoordinates(el.X, el.Y, excalidrawDimension(el.Width), excalidrawDimension(el.Height), el.Angle) ||
		!excalidrawColor.MatchString(el.StrokeColor) || !excalidrawColor.MatchString(el.BackgroundColor) ||
		len(el.Text) > 4096 || el.FontSize < 0 || el.FontSize > 256 || len(el.Points) > 128 ||
		!validExcalidrawValue(el.TextAlign, "", "left", "center", "right") ||
		!validExcalidrawValue(el.StrokeStyle, "", "solid", "dashed", "dotted") ||
		!validExcalidrawValue(el.FillStyle, "", "solid", "hachure", "cross-hatch", "zigzag") {
		return false
	}
	if (el.Type == "arrow" || el.Type == "line") && len(el.Points) == 1 {
		return false
	}
	for _, point := range el.Points {
		if len(point) != 2 || !validExcalidrawCoordinates(point...) {
			return false
		}
	}
	for _, binding := range []*excalidrawBinding{el.StartBinding, el.EndBinding} {
		if binding != nil && (!excalidrawID.MatchString(binding.ElementID) ||
			len(binding.FixedPoint) != 0 && (len(binding.FixedPoint) != 2 ||
				binding.FixedPoint[0] < 0 || binding.FixedPoint[0] > 1 || binding.FixedPoint[1] < 0 || binding.FixedPoint[1] > 1)) {
			return false
		}
	}
	for _, arrowhead := range []json.RawMessage{el.StartArrowhead, el.EndArrowhead} {
		if !validExcalidrawValue(string(arrowhead), "", "null", `"arrow"`, `"bar"`, `"dot"`, `"triangle"`, `"triangle_outline"`, `"diamond"`, `"diamond_outline"`, `"circle"`, `"circle_outline"`, `"crowfoot_one"`, `"crowfoot_many"`, `"crowfoot_one_or_many"`) {
			return false
		}
	}
	if el.Label != nil && (len(el.Label.Text) > 4096 || el.Label.FontSize < 0 || el.Label.FontSize > 256 ||
		!excalidrawColor.MatchString(el.Label.StrokeColor) ||
		!validExcalidrawValue(el.Label.TextAlign, "", "left", "center", "right") ||
		!validExcalidrawValue(el.Label.VerticalAlign, "", "top", "middle", "bottom")) {
		return false
	}
	return (el.Opacity == nil || *el.Opacity >= 0 && *el.Opacity <= 100) &&
		(el.Roughness == nil || *el.Roughness >= 0 && *el.Roughness <= 4) &&
		(el.StrokeWidth == nil || *el.StrokeWidth >= 0 && *el.StrokeWidth <= 64) &&
		(el.Roundness == nil || el.Roundness.Type >= 1 && el.Roundness.Type <= 3 && el.Roundness.Value >= 0 && el.Roundness.Value <= 1024)
}

func excalidrawDimension(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func validExcalidrawCoordinates(values ...float64) bool {
	for _, value := range values {
		if math.Abs(value) > maxExcalidrawCoordinate {
			return false
		}
	}
	return true
}

func validExcalidrawValue(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

// excalidrawSucceeded 核验绘图回执；WorkBuddy 的外层工具完成也可能包含 MCP 错误文本。
func excalidrawSucceeded(data map[string]any) bool {
	output := nestedObject(data, "rawOutput")
	if output == nil {
		output = nestedObject(data, "output")
	}
	if output["isError"] == true || output["is_error"] == true {
		return false
	}
	if checkpoint, _ := nestedObject(output, "structuredContent")["checkpointId"].(string); excalidrawID.MatchString(checkpoint) {
		return true
	}
	isReceipt := func(result map[string]any) bool {
		return strings.HasPrefix(firstDisplayString(result, "text"), `Diagram displayed! Checkpoint id: "`)
	}
	if isReceipt(output) {
		return true
	}
	for _, source := range []map[string]any{output, data} {
		items, _ := source["content"].([]any)
		for _, item := range items {
			object, _ := item.(map[string]any)
			if isReceipt(object) || isReceipt(nestedObject(object, "content")) {
				return true
			}
		}
	}
	return false
}
