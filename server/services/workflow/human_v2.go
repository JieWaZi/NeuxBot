package workflow

import (
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"io"
)

// ValidateHumanAnswer 校验冻结问题的类型、稳定候选键和多选数量。
func ValidateHumanAnswer(options map[string]any, value any) error {
	invalid := func() error { return status_error.WorkflowSchemaInvalid.WithField("field", "value") }
	kind, _ := options["kind"].(string)
	switch kind {
	case "text":
		if _, ok := value.(string); !ok {
			return invalid()
		}
	case "confirm":
		if _, ok := value.(bool); !ok {
			return invalid()
		}
	case "form":
		schema, ok := options["schema"].(map[string]any)
		if !ok {
			return invalid()
		}
		return workflowdsl.ValidateJSON(schema, value)
	case "select", "multiselect":
		choices, _ := options["choices"].([]any)
		custom, _ := options["allowCustom"].(bool)
		values := []any{value}
		if kind == "multiselect" {
			var ok bool
			values, ok = value.([]any)
			if !ok {
				return invalid()
			}
			minimum := 1
			maximum := len(choices)
			if n, ok := options["minSelections"].(float64); ok {
				minimum = int(n)
			}
			if n, ok := options["maxSelections"].(float64); ok {
				maximum = int(n)
			}
			if len(values) < minimum || len(values) > maximum {
				return invalid()
			}
		}
		seen := map[string]bool{}
		for _, entry := range values {
			encoded, err := json.Marshal(entry)
			if err != nil {
				return invalid()
			}
			key := string(encoded)
			if seen[key] {
				return invalid()
			}
			seen[key] = true
			found := false
			for _, raw := range choices {
				choice, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				candidate, _ := json.Marshal(choice["value"])
				if string(candidate) == key {
					found = true
					break
				}
			}
			if !found {
				if _, ok := entry.(string); !custom || !ok {
					return invalid()
				}
			}
		}
	default:
		return invalid()
	}
	if schema, ok := options["schema"].(map[string]any); ok {
		return workflowdsl.ValidateJSON(schema, value)
	}
	return nil
}

// validateHumanSnapshot 在等待问题建立前校验有界且唯一的不可变候选。
func validateHumanSnapshot(options map[string]any) error {
	kind, _ := options["kind"].(string)
	if kind == "" {
		return nil
	}
	if kind == "select" || kind == "multiselect" {
		choices, ok := options["choices"].([]any)
		if !ok || len(choices) == 0 || len(choices) > constants.WorkflowHumanMaximumChoices {
			return status_error.WorkflowSchemaInvalid.WithField("field", "choices")
		}
		encoded, err := json.Marshal(choices)
		if err != nil || len(encoded) > constants.WorkflowHumanChoicesLimitBytes {
			return status_error.WorkflowBudgetExceeded
		}
		seen := map[string]bool{}
		for _, raw := range choices {
			choice, ok := raw.(map[string]any)
			if !ok {
				return status_error.WorkflowSchemaInvalid
			}
			label, ok := choice["label"].(string)
			if !ok || label == "" {
				return status_error.WorkflowSchemaInvalid
			}
			value, exists := choice["value"].(string)
			if !exists || value == "" {
				return status_error.WorkflowSchemaInvalid
			}
			key, _ := json.Marshal(value)
			if seen[string(key)] {
				return status_error.WorkflowSchemaInvalid
			}
			seen[string(key)] = true
		}
	}
	if fallback, ok := options["defaultValue"]; ok {
		if err := ValidateHumanAnswer(options, fallback); err != nil {
			return err
		}
	}
	if options["onTimeout"] == "default" {
		if kind == "confirm" {
			return status_error.WorkflowSchemaInvalid
		}
		if _, ok := options["defaultValue"]; !ok {
			return status_error.WorkflowSchemaInvalid
		}
	}
	return nil
}

// freezeHumanChoices 将获准工作范围内候选文件读取为不可变问题快照。
func freezeHumanChoices(directory string, options map[string]any) error {
	reference, ok := options["choices"].(map[string]any)
	if !ok {
		return nil
	}
	fileRef, ok := reference["fileRef"].(string)
	if !ok || fileRef == "" {
		return status_error.WorkflowSchemaInvalid.WithField("field", "choices.fileRef")
	}
	file, err := workflowfiles.OpenScopeFile(directory, fileRef)
	if err != nil {
		return err
	}
	defer file.Close()
	bytes, err := io.ReadAll(io.LimitReader(file, constants.WorkflowHumanChoicesLimitBytes+1))
	if err != nil {
		return err
	}
	if len(bytes) > constants.WorkflowHumanChoicesLimitBytes {
		return status_error.WorkflowBudgetExceeded.WithField("budget", "human_choices")
	}
	var choices []any
	if json.Unmarshal(bytes, &choices) != nil {
		return status_error.WorkflowSchemaInvalid.WithField("field", "choices")
	}
	options["choices"] = choices
	options["choicesFile"] = map[string]any{"fileRef": fileRef, "digest": workflowstate.Digest(bytes)}
	return nil
}
