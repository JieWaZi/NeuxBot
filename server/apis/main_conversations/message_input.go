package main_conversations

import (
	"fmt"
	"path"
	"strings"
	"unicode"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// workInput 保留原执行正文和附件，并为公开交办生成仅含文件名的摘要。
func (b MainConversationInput) workInput() (execution.WorkInput, *execution.WorkOptions, string, string, error) {
	if b.Message == nil {
		if strings.TrimSpace(b.Content) == "" {
			return execution.WorkInput{}, nil, "", "", status_error.BadRequest
		}
		var digest string
		if b.StartWork {
			identity, err := requestidempotency.NewSubmission(b.SubmissionID, []any{b.Content, map[string]bool{"startWork": true}})
			if err != nil {
				return execution.WorkInput{}, nil, "", "", err
			}
			digest = identity.Hash
		}
		return execution.WorkInput{Actor: "user", Content: b.Content}, nil, b.Content, digest, nil
	}
	input, options := b.Message.WorkInput()
	if b.Content != "" || input.SteerExecutionID != "" || (strings.TrimSpace(input.Content) == "" && input.Workflow == nil) {
		return input, nil, "", "", status_error.BadRequest
	}
	options.HistorySourceChatID = b.HistorySourceChatID
	request := []any{input, options}
	if b.StartWork {
		request = append(request, map[string]bool{"startWork": true})
	}
	identity, err := requestidempotency.NewSubmission(b.SubmissionID, request)
	if err != nil {
		return input, nil, "", "", err
	}
	content, err := publicInputContent(input)
	return input, &options, content, identity.Hash, err
}

func publicInputContent(input execution.WorkInput) (string, error) {
	parts := []string{}
	if !input.AttachmentOnly && !input.CodeReferenceOnly && strings.TrimSpace(input.Content) != "" {
		parts = append(parts, input.Content)
	}
	for _, att := range input.Attachments {
		parts = append(parts, publicFilename(att.SourcePath))
	}
	for _, ref := range input.CodeReferences {
		name := publicFilename(ref.Path)
		if ref.StartLine > 0 {
			name += fmt.Sprintf(":%d", ref.StartLine)
			if ref.EndLine > ref.StartLine {
				name += fmt.Sprintf("-%d", ref.EndLine)
			}
		}
		parts = append(parts, name)
	}
	if len(parts) == 0 && input.Workflow != nil {
		parts = append(parts, input.Workflow.ID)
	}
	if len(parts) == 0 {
		return "", status_error.BadRequest
	}
	return strings.Join(parts, "\n\n"), nil
}

// richSourceMessageID 为有附件或代码引用的输入预留与执行不同的来源消息身份。
func richSourceMessageID(publicID string, input execution.WorkInput) string {
	if len(input.Attachments) == 0 && len(input.CodeReferences) == 0 {
		return ""
	}
	return (requestidempotency.Identity{ID: publicID}).ResourceID("main-conversation-user-input")
}

func publicFilename(value string) string {
	name := path.Base(strings.ReplaceAll(value, `\`, "/"))
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
}
