package dsl

import (
	"math"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/parser"
)

func (c *compiler) validateNodeOptions(node ast.Node, kind string, opts map[string]any) {
	allowed := map[string]bool{}
	for _, key := range []string{"id", "label", "phase", "inputs", "outputs"} {
		allowed[key] = true
	}
	if kind != "worker" {
		allowed["timeoutMs"] = true
	}
	var fields []string
	switch kind {
	case "worker":
		fields = []string{"resolve", "schema", "toolPolicy", "skippable", "retry"}
	case "askUser":
		fields = []string{"options", "multiSelect", "defaultValue", "allowSkip", "schema"}
	case "functionTask":
		fields = []string{"language", "code", "input", "outputSchema", "executionMode"}
	case "workflow", "startWorkflow":
		fields = []string{"args", "revisionId", "mode"}
	case "action":
		fields = []string{"input", "url", "method", "headers", "body", "secretHeaders", "args", "env", "secretEnv", "schema"}
	}
	for _, key := range fields {
		allowed[key] = true
	}
	for key := range opts {
		if !allowed[key] {
			c.fail(node, "OPTION_UNKNOWN", "unknown "+kind+" option: "+key)
		}
	}
	for _, key := range []string{"inputs", "outputs"} {
		if value, found := opts[key]; found {
			if !stringArray(value) {
				c.fail(node, "OPTION_TYPE_INVALID", key+" must be an array of strings")
			}
		}
	}
	for _, key := range []string{"skippable", "multiSelect", "allowSkip"} {
		if value, found := opts[key]; found {
			if _, ok := value.(bool); !ok {
				c.fail(node, "OPTION_TYPE_INVALID", key+" must be a boolean")
			}
		}
	}
	maximum := constants.WorkflowDefaultTimeoutMillis
	if kind == "functionTask" {
		maximum = constants.WorkflowFunctionMaxTimeoutMillis
	}
	if kind == "askUser" || kind == "workflow" || kind == "startWorkflow" {
		maximum = constants.WorkflowDefaultTimeoutMillis
	}
	if kind == "action" {
		maximum = constants.WorkflowScriptMaxTimeoutMillis
	}
	if value, found := opts["timeoutMs"]; found {
		number, ok := value.(float64)
		if !ok || number <= 0 || math.Trunc(number) != number || number > float64(maximum) {
			c.fail(node, "TIMEOUT_INVALID", "timeoutMs must be a positive integer within the host limit")
		}
	}
	for _, key := range []string{"schema", "outputSchema"} {
		if value, found := opts[key]; found {
			schema, ok := value.(map[string]any)
			if !ok {
				c.fail(node, "SCHEMA_UNSUPPORTED", key+" must be an object")
			} else if _, err := ResolveSchema(schema); err != nil {
				c.fail(node, "SCHEMA_UNSUPPORTED", key+" uses an invalid or unsupported schema")
			}
		}
	}
	if kind == "worker" {
		resolve, _ := opts["resolve"].(map[string]any)
		if value, found := resolve["permissionLevel"]; found {
			level, ok := value.(string)
			if !ok || !constants.SelectablePermissionLevel(constants.PermissionLevel(level)) {
				c.fail(node, "OPTION_TYPE_INVALID", "resolve.permissionLevel must be default, auto or full-access")
			}
		}
		if value, found := opts["toolPolicy"]; found {
			policy, ok := value.(map[string]any)
			if !ok {
				c.fail(node, "TOOL_POLICY_INVALID", "toolPolicy must be an object")
			} else {
				for key, entry := range policy {
					field := "connectorIds"
					if key == "nativeTools" {
						field = "capabilityIds"
					} else if key != "connectors" {
						c.fail(node, "TOOL_POLICY_INVALID", "unknown toolPolicy field: "+key)
						continue
					}
					scope, ok := entry.(map[string]any)
					if !ok {
						c.fail(node, "TOOL_POLICY_INVALID", "toolPolicy entry must be an object")
						continue
					}
					mode, _ := scope["mode"].(string)
					if mode == "inherit" {
						if len(scope) != 1 {
							c.fail(node, "TOOL_POLICY_INVALID", "inherit does not take an allowlist")
						}
					} else if mode == "allowlist" {
						if !stringArray(scope[field]) || len(scope) != 2 {
							c.fail(node, "TOOL_POLICY_INVALID", "allowlist requires the precise resource ID array")
						}
					} else {
						c.fail(node, "TOOL_POLICY_INVALID", "toolPolicy mode must be inherit or allowlist")
					}
				}
			}
		}
		if value, found := opts["retry"]; found {
			retry, ok := value.(map[string]any)
			if !ok {
				c.fail(node, "RETRY_INVALID", "retry must be an object")
			} else {
				attempts, ok := retry["maxAttempts"].(float64)
				safety, _ := retry["safety"].(string)
				if !ok || attempts < 1 || attempts > 5 || math.Trunc(attempts) != attempts || (safety != "pure" && safety != "idempotent") {
					c.fail(node, "RETRY_INVALID", "retry requires 1–5 maxAttempts and pure/idempotent safety")
				}
				if safety == "idempotent" {
					key, _ := retry["idempotencyKey"].(string)
					if key == "" {
						c.fail(node, "RETRY_INVALID", "idempotent retry requires an explicit key")
					}
				}
			}
		}
	}
	if kind == "askUser" {
		schema, structured := opts["schema"].(map[string]any)
		if structured {
			if _, found := opts["options"]; found {
				c.fail(node, "HUMAN_OPTIONS_INVALID", "schema and quick-answer options cannot be combined")
			}
			if _, found := opts["multiSelect"]; found {
				c.fail(node, "HUMAN_OPTIONS_INVALID", "schema declares its own array fields")
			}
		}
		if value, found := opts["options"]; found {
			options, ok := value.([]any)
			if !ok || len(options) > 64 {
				c.fail(node, "HUMAN_OPTIONS_INVALID", "options must contain at most 64 value/label objects")
			} else {
				seen := map[string]bool{}
				for _, item := range options {
					option, ok := item.(map[string]any)
					if !ok {
						c.fail(node, "HUMAN_OPTIONS_INVALID", "each option requires a value and label")
						continue
					}
					value, _ := option["value"].(string)
					label, _ := option["label"].(string)
					if value == "" || label == "" || seen[value] || len(option) != 2 {
						c.fail(node, "HUMAN_OPTIONS_INVALID", "option values must be unique and labels nonempty")
					}
					seen[value] = true
				}
			}
		}
		if value, found := opts["defaultValue"]; found {
			multi, _ := opts["multiSelect"].(bool)
			if structured {
				if err := ValidateJSON(schema, value); err != nil {
					c.fail(node, "HUMAN_DEFAULT_INVALID", "form default must match its schema")
				}
			} else if multi {
				if !stringArray(value) {
					c.fail(node, "HUMAN_DEFAULT_INVALID", "multi-select default must be an array of strings")
				}
			} else if _, ok := value.(string); !ok {
				c.fail(node, "HUMAN_DEFAULT_INVALID", "single-select default must be a string")
			}
		}
	}
	if kind == "functionTask" {
		if opts["language"] == "javascript" {
			if code, ok := opts["code"].(string); ok && code != "" {
				c.validateJavaScriptFunction(node, code)
			}
		}
		if mode, found := opts["executionMode"]; found && mode != "pure" && mode != "trusted_local" {
			c.fail(node, "FUNCTION_MODE_INVALID", "executionMode must be pure or trusted_local")
		}
	}
	if kind == "startWorkflow" {
		if mode := opts["mode"]; mode != "joined" && mode != "detached" {
			c.fail(node, "CHILD_MODE_REQUIRED", "startWorkflow requires explicit joined or detached mode")
		}
	}
}

// validateJavaScriptFunction 只解析静态代码，不在保存或接纳时执行用户程序。
func (c *compiler) validateJavaScriptFunction(node ast.Node, code string) {
	program, err := parser.ParseFile(nil, "workflow-function.js", code, 0)
	if err != nil {
		c.fail(node, "FUNCTION_CODE_INVALID", "JavaScript Function code must define function main(input): "+err.Error())
		return
	}
	for _, statement := range program.Body {
		declaration, ok := statement.(*ast.FunctionDeclaration)
		if ok && declaration.Function.Name != nil && declaration.Function.Name.Name.String() == "main" {
			if declaration.Function.Async || declaration.Function.Generator {
				c.fail(node, "FUNCTION_MAIN_INVALID", "Function main(input) must synchronously return JSON")
			}
			return
		}
	}
	c.fail(node, "FUNCTION_MAIN_REQUIRED", "JavaScript Function code must define function main(input) and return JSON; a bare function body is not an entry point")
}

func stringArray(value any) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if _, ok := item.(string); !ok {
			return false
		}
	}
	return true
}
