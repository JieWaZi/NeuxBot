package dsl

import (
	"encoding/json"
	"math"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/dop251/goja/ast"
)

func (c *compiler) validateHumanOptions(node ast.Node, opts map[string]any) {
	kind, _ := opts["kind"].(string)
	if kind == "" {
		kind = "text"
	}
	if kind != "text" && kind != "select" && kind != "multiselect" && kind != "form" && kind != "confirm" {
		c.fail(node, "HUMAN_OPTIONS_INVALID", "kind must be text, select, multiselect, form or confirm")
		return
	}
	choices, hasChoices := opts["choices"]
	call, _ := node.(*ast.CallExpression)
	choicesExpression := ""
	if call != nil {
		choicesExpression = c.optionFragment(call, 1, "choices")
	}
	if (kind == "select" || kind == "multiselect") && choicesExpression == "" {
		c.fail(node, "HUMAN_OPTIONS_INVALID", "select and multiselect require choices")
	}
	schema, hasSchema := opts["schema"].(map[string]any)
	if hasSchema && kind != "form" {
		c.fail(node, "HUMAN_OPTIONS_INVALID", "schema is available only for form questions")
	}
	if kind == "form" && !hasSchema {
		c.fail(node, "HUMAN_OPTIONS_INVALID", "form requires a schema")
	}
	if hasChoices && kind != "select" && kind != "multiselect" {
		c.fail(node, "HUMAN_OPTIONS_INVALID", "choices are available only for select and multiselect")
	}
	for _, field := range []string{"minSelections", "maxSelections"} {
		if value, found := opts[field]; found {
			n, ok := value.(float64)
			if kind != "multiselect" || !ok || n < 0 || n > float64(constants.WorkflowHumanMaximumChoices) || math.Trunc(n) != n {
				c.fail(node, "HUMAN_OPTIONS_INVALID", field+" requires a bounded nonnegative multiselect integer")
			}
		}
	}
	minimum := float64(1)
	if n, ok := opts["minSelections"].(float64); ok {
		minimum = n
	}
	if maximum, ok := opts["maxSelections"].(float64); ok && maximum < minimum {
		c.fail(node, "HUMAN_OPTIONS_INVALID", "maxSelections must be at least minSelections")
	}
	keys := map[string]bool{}
	staticChoices := false
	if hasChoices {
		if items, ok := choices.([]any); ok {
			staticChoices = true
			wire, _ := json.Marshal(items)
			if len(items) == 0 || len(items) > constants.WorkflowHumanMaximumChoices || len(wire) > constants.WorkflowHumanChoicesLimitBytes {
				c.fail(node, "HUMAN_OPTIONS_INVALID", "choices exceed the inline candidate budget")
			}
			for _, item := range items {
				option, ok := item.(map[string]any)
				value, valueOK := option["value"].(string)
				label, labelOK := option["label"].(string)
				if !ok || !valueOK || !labelOK || value == "" || label == "" || len(option) != 2 || keys[value] {
					c.fail(node, "HUMAN_OPTIONS_INVALID", "choices require unique nonempty string values and labels")
				}
				keys[value] = true
			}
			if kind == "multiselect" && minimum > float64(len(items)) {
				c.fail(node, "HUMAN_OPTIONS_INVALID", "minSelections exceeds candidate count")
			}
		} else if ref, ok := choices.(map[string]any); !ok || ref["fileRef"] == nil {
			c.fail(node, "HUMAN_OPTIONS_INVALID", "choices require an array or a candidate fileRef")
		}
	}
	if value, found := opts["defaultValue"]; found {
		valid := true
		switch kind {
		case "text":
			_, valid = value.(string)
		case "confirm":
			_, valid = value.(bool)
		case "form":
			valid = ValidateJSON(schema, value) == nil
		case "select":
			key, ok := value.(string)
			custom, _ := opts["allowCustom"].(bool)
			valid = ok && (!staticChoices || custom || keys[key])
		case "multiselect":
			items, ok := value.([]any)
			valid = ok && float64(len(items)) >= minimum
			maximum := float64(constants.WorkflowHumanMaximumChoices)
			if staticChoices {
				maximum = float64(len(keys))
			}
			if n, ok := opts["maxSelections"].(float64); ok {
				maximum = n
			}
			valid = valid && float64(len(items)) <= maximum
			seen := map[string]bool{}
			custom, _ := opts["allowCustom"].(bool)
			for _, item := range items {
				key, ok := item.(string)
				valid = valid && ok && !seen[key] && (!staticChoices || custom || keys[key])
				seen[key] = true
			}
		}
		if !valid {
			c.fail(node, "HUMAN_DEFAULT_INVALID", "defaultValue must satisfy the question contract")
		}
	}
	if mode, found := opts["onTimeout"]; found {
		if mode != "fail" && mode != "skip" && mode != "default" {
			c.fail(node, "HUMAN_OPTIONS_INVALID", "onTimeout must be fail, skip or default")
		}
		if _, deadline := opts["timeoutMs"]; !deadline {
			c.fail(node, "HUMAN_OPTIONS_INVALID", "onTimeout requires a real timeoutMs deadline")
		}
		if mode == "default" {
			_, found := opts["defaultValue"]
			if !found && call != nil {
				found = c.optionFragment(call, 1, "defaultValue") != ""
			}
			if !found || kind == "confirm" {
				c.fail(node, "HUMAN_DEFAULT_INVALID", "timeout default requires a legal defaultValue and cannot approve confirm")
			}
		}
	}
}
