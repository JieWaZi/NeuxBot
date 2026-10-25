package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/neux-knowledge/core"
	"math"
	"strconv"
)

// ReadBindings 对每个读、搜索和来源操作重复校验冻结范围及当前关系。
func ReadBindings(engine *core.Engine, scope Scope, current func(context.Context, string) ([]ResourceRef, error)) []agentcapability.Binding {
	return ProviderReadBindings(LocalReadProvider{Engine: engine}, scope, current)
}

// ProviderReadBindings 在完整资源引用上共用同一冻结与当前授权校验。
func ProviderReadBindings(provider ReadProvider, scope Scope, current func(context.Context, string) ([]ResourceRef, error)) []agentcapability.Binding {
	authorize := func(ctx context.Context, r agentcapability.Request, op string) (ResourceRef, error) {
		id, _ := r.Params["libraryId"].(string)
		backend, _ := r.Params["backend"].(string)
		connection, _ := r.Params["connectionId"].(string)
		if backend == "" {
			backend = "local"
		}
		if connection == "" && backend == "local" {
			connection = "local"
		}
		ref := ResourceRef{Backend: backend, ConnectionID: connection, KnowledgeBaseID: id}
		if err := scope.Authorize(ctx, ref, op, current); err != nil {
			return ResourceRef{}, err
		}
		return ref, nil
	}
	bindings := []agentcapability.Binding{
		{Operation: "knowledge.libraries.list", Call: func(ctx context.Context, _ agentcapability.Request) (agentcapability.Result, error) {
			items := []map[string]any{}
			now, err := current(ctx, scope.ProjectID)
			if err != nil {
				return agentcapability.Result{}, err
			}
			cachedCurrent := func(context.Context, string) ([]ResourceRef, error) { return now, nil }
			for _, ref := range scope.Resources {
				if err := scope.Authorize(ctx, ref, "read", cachedCurrent); err != nil {
					if errors.Is(err, status_error.KnowledgeForbidden) {
						continue
					}
					return agentcapability.Result{}, err
				}
				lib, err := provider.Library(ctx, ref)
				if err != nil {
					return agentcapability.Result{}, err
				}
				items = append(items, map[string]any{"ref": ref, "name": lib.Name, "kind": lib.Kind, "wikiLanguage": lib.WikiLanguage})
			}
			return agentcapability.Result{Data: map[string]any{"items": items, "total": len(items)}}, nil
		}},
		{Operation: "knowledge.pages.search", Call: func(ctx context.Context, r agentcapability.Request) (agentcapability.Result, error) {
			id, err := authorize(ctx, r, "search")
			if err != nil {
				return agentcapability.Result{}, err
			}
			q, _ := r.Params["query"].(string)
			offset := number(r.Params, "offset")
			if offset < 0 {
				return agentcapability.Result{}, status_error.KnowledgeInvalid
			}
			rows, total, err := provider.Pages(ctx, id, core.PageQuery{LibraryID: id.KnowledgeBaseID, Query: q, Status: "published", Offset: offset, Limit: 50})
			if err != nil {
				return agentcapability.Result{}, err
			}
			items := []map[string]any{}
			for _, p := range rows {
				items = append(items, map[string]any{"id": p.ID, "title": p.Title, "slug": p.Slug, "pageType": p.PageType, "summary": p.Summary, "version": p.Version})
			}
			data := map[string]any{"ref": id, "items": items, "total": total}
			// 页码仅用于 Wiki 列表；原件检索提供首屏有界的五条完整上下文。
			if search, ok := provider.(DocumentReadProvider); ok && offset == 0 && q != "" {
				chunks, mode, e := search.SearchChunks(ctx, id, q)
				if e != nil {
					return agentcapability.Result{}, e
				}
				if mode != "" {
					data["chunks"] = chunks
					data["retrievalMode"] = mode
				}
			}
			return agentcapability.Result{Data: data}, nil
		}},
		{Operation: "knowledge.pages.get", Call: func(ctx context.Context, r agentcapability.Request) (agentcapability.Result, error) {
			id, err := authorize(ctx, r, "read")
			if err != nil {
				return agentcapability.Result{}, err
			}
			pid, _ := r.Params["pageId"].(string)
			if (pid == "") == (stringParam(r.Params, "slug") == "") {
				return agentcapability.Result{}, status_error.KnowledgeInvalid
			}
			p, err := provider.Page(ctx, id, pid, stringParam(r.Params, "slug"))
			if err != nil {
				return agentcapability.Result{}, err
			}
			if len(p.Content) > 64<<10 {
				return agentcapability.Result{}, status_error.KnowledgeUnsupported
			}
			return agentcapability.Result{Data: map[string]any{"ref": id, "page": p}}, nil
		}},
		{Operation: "knowledge.sources.get", Call: func(ctx context.Context, r agentcapability.Request) (agentcapability.Result, error) {
			id, err := authorize(ctx, r, "source")
			if err != nil {
				return agentcapability.Result{}, err
			}
			sid, _ := r.Params["sourceVersionId"].(string)
			p, err := provider.Source(ctx, id, sid)
			if err != nil {
				return agentcapability.Result{}, err
			}
			return agentcapability.Result{Data: map[string]any{"ref": id, "source": p}}, nil
		}},
	}
	for _, operation := range []string{"knowledge.libraries.get", "knowledge.sources.list", "knowledge.sources.read", "knowledge.pages.list", "knowledge.themes.list", "knowledge.index.get", "knowledge.revisions.list", "knowledge.revisions.get", "knowledge.jobs.list", "knowledge.jobs.get", "knowledge.audit.get", "knowledge.activity.list", "knowledge.export.get"} {
		bindings = append(bindings, agentcapability.Binding{Operation: operation, Call: func(ctx context.Context, r agentcapability.Request) (agentcapability.Result, error) {
			op := "read"
			if operation == "knowledge.sources.list" || operation == "knowledge.sources.read" {
				op = "source"
			}
			ref, err := authorize(ctx, r, op)
			if err != nil {
				return agentcapability.Result{}, err
			}
			params, err := readParams(operation, r.Params)
			if err != nil {
				return agentcapability.Result{}, err
			}
			extended, ok := provider.(ExtendedReadProvider)
			if !ok {
				return agentcapability.Result{}, status_error.KnowledgeUnsupported
			}
			data, err := extended.ExtendedRead(ctx, ref, operation, params)
			if err != nil {
				return agentcapability.Result{}, err
			}
			data["ref"] = ref
			return agentcapability.Result{Data: data}, nil
		}})
	}
	return bindings
}

// readParams 在调用提供者前校验数值与相互依赖的读取参数，不修改原请求。
func readParams(operation string, input map[string]any) (map[string]any, error) {
	params := make(map[string]any, len(input)+2)
	for k, v := range input {
		params[k] = v
	}
	for _, field := range []string{"offset", "limit", "version"} {
		v, exists := params[field]
		if !exists {
			continue
		}
		n, ok := readInteger(v)
		if !ok || n < 0 || (field != "offset" && n == 0) || (field == "limit" && n > 100) {
			return nil, status_error.KnowledgeInvalid
		}
		params[field] = n
	}
	if _, ok := params["offset"]; !ok {
		params["offset"] = 0
	}
	if _, ok := params["limit"]; !ok {
		params["limit"] = 50
	}
	for _, field := range []string{"query", "state", "sort", "chunkId", "sourceVersionId", "pageId", "jobId", "cursor", "action", "outcome", "actorUserId", "pageType", "themeId"} {
		if v, ok := params[field]; ok {
			if _, ok = v.(string); !ok {
				return nil, status_error.KnowledgeInvalid
			}
		}
	}
	for _, field := range []string{"pageIds", "pageTypes"} {
		if v, ok := params[field]; ok {
			values, valid := readStrings(v)
			if !valid || len(values) > 1000 {
				return nil, status_error.KnowledgeInvalid
			}
			params[field] = values
		}
	}
	required := ""
	switch operation {
	case "knowledge.sources.read":
		required = "sourceVersionId"
	case "knowledge.revisions.list", "knowledge.revisions.get":
		required = "pageId"
	case "knowledge.jobs.get":
		required = "jobId"
	}
	if required != "" && stringParam(params, required) == "" {
		return nil, status_error.KnowledgeInvalid
	}
	if operation == "knowledge.revisions.get" && number(params, "version") < 1 {
		return nil, status_error.KnowledgeInvalid
	}
	if operation == "knowledge.sources.read" && stringParam(params, "query") != "" && stringParam(params, "chunkId") != "" {
		return nil, status_error.KnowledgeInvalid
	}
	if operation == "knowledge.activity.list" {
		var cursor uint64
		if value, exists := params["afterId"]; exists {
			if text, ok := value.(string); ok {
				parsed, err := strconv.ParseUint(text, 10, 64)
				if err != nil {
					return nil, status_error.KnowledgeInvalid
				}
				cursor = parsed
			} else {
				n, ok := readInteger(value)
				if !ok || n < 0 || uint64(n) > 1<<53-1 {
					return nil, status_error.KnowledgeInvalid
				}
				cursor = uint64(n)
			}
		}
		params["afterId"] = cursor
	}
	return params, nil
}

func readInteger(value any) (int, bool) {
	switch n := value.(type) {
	case int:
		return n, true
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || math.Trunc(n) != n || n < 0 || n > float64(1<<53-1) {
			return 0, false
		}
		return int(n), true
	case json.Number:
		n64, err := n.Int64()
		return int(n64), err == nil && int64(int(n64)) == n64
	}
	return 0, false
}

func readStrings(value any) ([]string, bool) {
	var out []string
	switch values := value.(type) {
	case []string:
		out = append([]string{}, values...)
	case []any:
		for _, v := range values {
			text, ok := v.(string)
			if !ok {
				return nil, false
			}
			out = append(out, text)
		}
	default:
		return nil, false
	}
	seen := map[string]bool{}
	for _, text := range out {
		if text == "" || seen[text] {
			return nil, false
		}
		seen[text] = true
	}
	return out, true
}

func stringParam(params map[string]any, key string) string { v, _ := params[key].(string); return v }

// number 读取只读查询的有界数值参数；未知值由后续查询校验处理。
func number(values map[string]any, key string) int {
	switch value := values[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case json.Number:
		parsed, _ := value.Int64()
		return int(parsed)
	}
	return 0
}
