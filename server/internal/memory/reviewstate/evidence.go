package reviewstate

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

const (
	// MaxTurns 是单批可处理的最大成功轮次。
	MaxTurns = 20
	// DefaultEveryTurns 是新配置的默认整理频率。
	DefaultEveryTurns = 20
	// IdleTurns 是空闲时允许触发的最小成功轮次。
	IdleTurns = 3
	// IdleDelay 是最后完成轮次到空闲触发的等待时间。
	IdleDelay = 15 * time.Minute
	// EvidenceBytes 是模型输入中证据段的总序列化预算。
	EvidenceBytes = 24 << 10
	// MemoryBytes 为相关记忆保留有界空间。
	MemoryBytes = 4 << 10
	// ToolBytes 为结构化工具步骤保留有界空间。
	ToolBytes     = 6 << 10
	turnTextBytes = 6 << 10
)

var privateHeader = regexp.MustCompile(`(?i)(["']?(?:authorization|proxy-authorization|cookie|set-cookie)["']?[ \t]*[:=])[^\r\n]+`)
var bearer = regexp.MustCompile(`(?i)(bearer[ \t]+)[^ \t\r\n,;"']+`)
var quotedCredential = regexp.MustCompile(`(?i)(["']?(?:x[-_ ]?api[-_ ]?key|api[-_ ]?key|access[-_ ]?token|refresh[-_ ]?token|token|password|secret)["']?[ \t]*[=:][ \t]*)(?:"(?:\\.|[^"\\\r\n])*"|'(?:\\.|[^'\\\r\n])*'|["'][^\r\n]*)`)
var credential = regexp.MustCompile(`(?i)(["']?(?:x[-_ ]?api[-_ ]?key|api[-_ ]?key|access[-_ ]?token|refresh[-_ ]?token|token|password|secret)["']?[ \t]*[=:][ \t]*)[^ \t\r\n,;"']+`)
var token = regexp.MustCompile("(?i)(?:sk-(?:proj-|ant-)?[a-z0-9_-]{12,}|gh[pousr]_[a-z0-9]{12,}|github_pat_[a-z0-9_]{12,}|akia[a-z0-9]{16}|eyj[a-z0-9_\\-]+\\.[a-z0-9_\\-]+\\.[a-z0-9_\\-]+)")

// ToolStep 是允许进入技能学习的标准化步骤。
type ToolStep struct {
	// ID 是原工具调用的来源标识。
	ID string `json:"ref,omitempty"`
	// Name 是稳定工具类型。
	Name string `json:"name"`
	// Failed 表示持久化工具结果失败。
	Failed bool `json:"failed,omitempty"`
	// Summary 是安全投影提供的简短结果。
	Summary string `json:"summary,omitempty"`
}

// Turn 是来自持久终态的复盘证据。
type Turn struct {
	// Sequence 是权威终态位置。
	Sequence int64 `json:"-"`
	// UserID 是用户原消息身份。
	UserID string `json:"userId"`
	// AssistantID 是助手公开结果身份。
	AssistantID string `json:"assistantId,omitempty"`
	// User 是原输入文本。
	User string `json:"user"`
	// Assistant 是已确认的最终答复。
	Assistant string `json:"assistant,omitempty"`
	// Steps 是有界结构化操作步骤。
	Steps []ToolStep `json:"steps,omitempty"`
	// At 是终态完成时间。
	At time.Time `json:"-"`
	// Memory 表明可作为记忆来源。
	Memory bool `json:"memory,omitempty"`
	// Skill 表明可作为技能学习来源。
	Skill bool `json:"skill,omitempty"`
}

// canonicalSkillSources translates tool references to their owning public message.
func canonicalSkillSources(turns []Turn, ids []string) []string {
	owners := make(map[string]string)
	for _, turn := range turns {
		for _, step := range turn.Steps {
			owners[step.ID] = turn.AssistantID
		}
	}
	sources := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if owner := owners[id]; owner != "" {
			id = owner
		}
		if !seen[id] {
			sources = append(sources, id)
			seen[id] = true
		}
	}
	return sources
}

// ReadyThrough 按既有冻结边界、轮次和空闲条件决定本次触发上界。
func ReadyThrough(turns []Turn, through int64, everyTurns int, active bool, now time.Time) int64 {
	if through > 0 {
		return through
	}
	if len(turns) >= everyTurns {
		return turns[everyTurns-1].Sequence
	}
	if !active && len(turns) >= IdleTurns && !now.Before(turns[len(turns)-1].At.Add(IdleDelay)) {
		return turns[len(turns)-1].Sequence
	}
	return 0
}

// Redact 删除支持识别的常见凭证。包含私钥区块的证据整体跳过。
func Redact(text string) string {
	if strings.Contains(text, "PRIVATE KEY-----") {
		return ""
	}
	text = privateHeader.ReplaceAllString(text, "${1} [已脱敏]")
	text = quotedCredential.ReplaceAllString(text, "${1}[已脱敏]")
	text = bearer.ReplaceAllString(text, "${1}[已脱敏]")
	text = credential.ReplaceAllString(text, "${1}[已脱敏]")
	return token.ReplaceAllString(text, "[已脱敏]")
}

func prefix(text string, size int) string {
	if len(text) <= size {
		return text
	}
	for size > 0 && !utf8.RuneStart(text[size]) {
		size--
	}
	return text[:size]
}

// BuildEvidence 将本地终态投影成只包含已授权字段的有界证据。
func BuildEvidence(turns []Turn) []Turn {
	values := make([]Turn, 0, min(len(turns), MaxTurns))
	toolBudget := ToolBytes
	for _, turn := range turns[:min(len(turns), MaxTurns)] {
		turn.User = prefix(Redact(turn.User), turnTextBytes)
		if !turn.Skill {
			turn.Assistant, turn.AssistantID, turn.Steps = "", "", nil
		} else {
			turn.Assistant = prefix(Redact(reviewAssistantText(turn.Assistant)), turnTextBytes)
			steps := make([]ToolStep, 0, min(len(turn.Steps), 24))
			for _, step := range lessonSteps(turn.Steps) {
				step.Name = prefix(Redact(step.Name), 100)
				step.Summary = prefix(Redact(step.Summary), 240)
				data, _ := json.Marshal(step)
				if step.Name != "" && len(data) <= toolBudget {
					steps = append(steps, step)
					toolBudget -= len(data)
				}
			}
			turn.Steps = steps
		}
		values = append(values, turn)
	}
	return values
}

// fitEvidence 以实际 JSON 大小拆批，防止多字节及转义突破输入上限。
func fitEvidence(turns []Turn, budget int, projectedSize func([]Turn) int) ([]Turn, error) {
	fit := make([]Turn, 0, len(turns))
	for _, turn := range turns {
		if projectedSize(append(fit, turn)) > budget {
			break
		}
		fit = append(fit, turn)
	}
	if len(turns) > 0 && len(fit) == 0 {
		// An unusually escaped turn can exceed the batch despite individual field limits.
		turn := turns[0]
		turn.User, turn.Assistant, turn.Steps = prefix(turn.User, 1024), prefix(turn.Assistant, 1024), nil
		candidate := []Turn{turn}
		if projectedSize(candidate) > budget {
			return nil, status_error.RuntimeProtocolIncompatible
		}
		fit = candidate
	}
	return fit, nil
}

// reviewAssistantText leaves quoted raw blocks in the source Chat.
func reviewAssistantText(text string) string {
	var output strings.Builder
	fence := ""
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence = trimmed[:3]
			continue
		}
		if strings.HasPrefix(trimmed, ">") {
			continue
		}
		output.WriteString(line)
		output.WriteByte('\n')
	}
	return strings.TrimSpace(output.String())
}

// lessonSteps retains failures and their immediate recovery before filling with recent steps.
func lessonSteps(items []ToolStep) []ToolStep {
	if len(items) <= 24 {
		return items
	}
	keep := make(map[int]bool, 24)
	for i, step := range items {
		if step.Failed && len(keep) < 24 {
			keep[i] = true
			if i+1 < len(items) && len(keep) < 24 {
				keep[i+1] = true
			}
		}
	}
	for i := len(items) - 1; i >= 0 && len(keep) < 24; i-- {
		keep[i] = true
	}
	result := make([]ToolStep, 0, len(keep))
	for i, step := range items {
		if keep[i] {
			result = append(result, step)
		}
	}
	return result
}
