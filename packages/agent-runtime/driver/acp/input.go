package acp

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

const maxAttachmentBytes = 20 << 20

// promptBlocks 保留固定代码快照和文件引用，只在握手明确支持时附加图片内容。
func promptBlocks(input contract.Input, prefix string, capabilities sdk.PromptCapabilities) ([]sdk.ContentBlock, error) {
	var text strings.Builder
	text.WriteString(prefix)
	text.WriteString(input.Text)
	if len(input.Attachments) > 0 {
		text.WriteString("\n\nAttachments:\n")
		for _, attachment := range input.Attachments {
			path := attachment.DisplayPath
			if path == "" {
				path = attachment.Path
			}
			fmt.Fprintf(&text, "- %s (%s)\n", path, attachment.Name)
		}
	}
	appendCodeReferences(&text, input.CodeReferences)
	if len(input.SelectedSkills) > 0 {
		fmt.Fprintf(&text, "\n\nUse the selected skills: %s.\n", strings.Join(input.SelectedSkills, ", "))
	}
	if len(input.SelectedMCP) > 0 {
		fmt.Fprintf(&text, "\n\nUse the selected MCP connections: %s.\n", strings.Join(input.SelectedMCP, ", "))
	}
	blocks := []sdk.ContentBlock{sdk.TextBlock(text.String())}
	if !capabilities.Image {
		return blocks, nil
	}
	for _, attachment := range input.Attachments {
		if !strings.HasPrefix(attachment.MIMEType, "image/") {
			continue
		}
		file, err := os.Open(attachment.Path)
		if err != nil {
			return nil, fault(contract.FaultPreparation, "input", "ACP image attachment cannot be opened", err)
		}
		data, readErr := io.ReadAll(io.LimitReader(file, maxAttachmentBytes+1))
		closeErr := file.Close()
		if readErr != nil {
			return nil, fault(contract.FaultPreparation, "input", "ACP image attachment cannot be read", readErr)
		}
		if closeErr != nil {
			return nil, fault(contract.FaultPreparation, "input", "ACP image attachment cannot be closed", closeErr)
		}
		if len(data) > maxAttachmentBytes {
			return nil, fault(contract.FaultLimit, "input", "ACP image attachment exceeds byte budget", nil)
		}
		blocks = append(blocks, sdk.ImageBlock(base64.StdEncoding.EncodeToString(data), attachment.MIMEType))
	}
	return blocks, nil
}

// appendCodeReferences 保留片段发送时的文本，整文件只提交原始引用路径。
func appendCodeReferences(text *strings.Builder, references []contract.CodeReference) {
	for _, reference := range references {
		if reference.StartLine == 0 && reference.EndLine == 0 && reference.Content == "" {
			fmt.Fprintf(text, "\n\nReferenced file: %s\n", reference.Path)
			continue
		}
		fmt.Fprintf(text, "\n\nCode reference: %s:%d-%d\n<code-reference>\n%s\n</code-reference>\n", reference.Path, reference.StartLine, reference.EndLine, reference.Content)
	}
}
