package builtinrolepreset

import (
	"fmt"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
)

// WakerSeed 描述空库 bootstrap 时需要写入的默认伙伴。
type WakerSeed struct {
	// ID 是数据库使用的稳定伙伴标识。
	ID string
	// Name 是用户识别伙伴时看到的名称。
	Name string
}

// definitions 聚合全部内置角色预设。
//
// 新增内置角色时：
//  1. 在同目录新增 <role_key>.go，声明稳定 ID、Projection 展示摘要、Agent 档案与 definition 构造函数；
//  2. 在下方切片中注册该构造函数；
//  3. 若需要首次启动即可用的默认伙伴，在 Definition 上填写 DefaultWaker。
var definitions = []Definition{
	personalAssistantDefinition(),
	wikiResearcherDefinition(),
}

func init() {
	if err := validateDefinitions(definitions); err != nil {
		panic(err)
	}
}

func validateDefinitions(items []Definition) error {
	rolePresetIDs := make(map[string]struct{}, len(items))
	roleNames := make(map[string]struct{}, len(items))
	wakerIDs := make(map[string]struct{}, len(items))
	for _, definition := range items {
		if definition.ID == "" {
			return invalidDefinition("builtin role preset id is required")
		}
		if _, exists := rolePresetIDs[definition.ID]; exists {
			return invalidDefinition("duplicate builtin role preset id %q", definition.ID)
		}
		rolePresetIDs[definition.ID] = struct{}{}
		if definition.RoleName == "" {
			return invalidDefinition("builtin role preset %q role name is required", definition.ID)
		}
		if _, exists := roleNames[definition.RoleName]; exists {
			return invalidDefinition("duplicate builtin role preset name %q", definition.RoleName)
		}
		roleNames[definition.RoleName] = struct{}{}
		if definition.DefaultAvatar == "" {
			return invalidDefinition("builtin role preset %q default avatar is required", definition.ID)
		}
		if err := validateDocuments(definition); err != nil {
			return err
		}
		if definition.DefaultWaker == nil {
			continue
		}
		if definition.DefaultWaker.ID == "" || definition.DefaultWaker.Name == "" {
			return invalidDefinition(
				"builtin role preset %q default waker id and name are required",
				definition.ID,
			)
		}
		if _, exists := wakerIDs[definition.DefaultWaker.ID]; exists {
			return invalidDefinition("duplicate builtin waker id %q", definition.DefaultWaker.ID)
		}
		wakerIDs[definition.DefaultWaker.ID] = struct{}{}
	}
	return nil
}

func validateDocuments(definition Definition) error {
	if projectionEmpty(definition.Projection) {
		return invalidDefinition("builtin role preset %q projection is required", definition.ID)
	}
	for _, kind := range profile.EditableKinds() {
		content, exists := definition.Documents[kind]
		if !exists {
			return invalidDefinition(
				"builtin role preset %q missing document kind %q",
				definition.ID,
				kind,
			)
		}
		if err := profile.ValidateContent(content); err != nil {
			return err
		}
		if err := validateAgentOnlyDocument(content); err != nil {
			return err
		}
	}
	return nil
}

func validateAgentOnlyDocument(content string) error {
	for _, heading := range []string{"## 擅长领域", "## 工作风格", "## 工作方式"} {
		if strings.Contains(content, heading) {
			return invalidDefinition("agent document must not contain %q", heading)
		}
	}
	return nil
}

// invalidDefinition 为内置目录校验提供稳定错误，并保留英文技术原因。
func invalidDefinition(format string, values ...any) error {
	return status_error.BuiltinRolePresetDefinitionInvalid.WithParams(map[string]interface{}{
		"Reason": fmt.Sprintf(format, values...),
	})
}
