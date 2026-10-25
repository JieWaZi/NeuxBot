// integration-contract 将运行时公开登记输出给 OpenAPI 投影脚本。
package main

import (
	"context"
	"encoding/json"
	"go/types"
	"os"
	"regexp"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/apis/automatic_tasks"
	"github.com/JieWaZi/NeuxBot/server/apis/integration_knowledge"
	"github.com/go-courier/oas"
	"github.com/go-courier/packagesx"
	"github.com/shrewx/ginx/pkg/openapi"
)

func main() {
	routes := append(integration_knowledge.PublishedRoutes(), automatic_tasks.PublishedRoutes()...)
	pkg, err := packagesx.Load("./cmd/ally-core")
	if err != nil {
		panic(err)
	}
	scanner := openapi.NewOperatorScanner(pkg)
	document := oas.NewOpenAPI()
	parameters := regexp.MustCompile(`/[:*]([^/]+)`)
	for _, route := range routes {
		var typeName *types.TypeName
		for _, path := range []string{"github.com/JieWaZi/NeuxBot/server/apis/integration_knowledge", "github.com/JieWaZi/NeuxBot/server/apis/automatic_tasks"} {
			candidate := packagesx.NewPackage(pkg.Pkg(path)).TypeName(route.OperationID)
			if candidate != nil {
				typeName = candidate
				break
			}
		}
		if typeName == nil {
			panic("公开 Operator 不在生成源中: " + route.OperationID)
		}
		operator := scanner.Operator(context.Background(), typeName)
		operation := &oas.Operation{}
		operator.BindOperation(route.Method, operation, true)
		path := parameters.ReplaceAllString(route.Path, "/{$1}")
		document.AddOperation(oas.HttpMethod(strings.ToLower(route.Method)), path, operation)
	}
	scanner.BindSchemas(document)
	if len(os.Args) != 2 {
		panic("contract output path required")
	}
	raw, err := json.Marshal(map[string]any{"routes": routes, "document": document})
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(os.Args[1], raw, 0600); err != nil {
		panic(err)
	}
}
