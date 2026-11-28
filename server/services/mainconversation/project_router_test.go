package mainconversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestProjectRouterUsesOnlySelectionsAuthorizedByCurrentInput(t *testing.T) {
	base := t.TempDir()
	directory := filepath.Join(base, "project with spaces")
	cases := []struct {
		// name 描述选择项目时需要守住的边界。
		name string
		// input 是本次用户输入。
		input string
		// prior 是上一次未确定范围的交办。
		prior string
		// response 模拟一次真实模型生成的 JSON。
		response string
		// project 是获准派工项目，空值说明没有授予项目。
		project string
		// directory 是获准派工目录，空值说明没有授予目录。
		directory string
		// clarify 表示只应在原会话询问。
		clarify bool
		// invalid 表示响应本身无法解析。
		invalid bool
	}{
		{name: "unique named project", input: "Inspect README in ALPHA.", response: `{"action":"dispatch","scope":"project","choice":1}`, project: "internal-alpha"},
		{name: "second named project", input: "检查 Beta 项目的 README", response: `{"action":"dispatch","scope":"project","choice":2}`, project: "internal-beta"},
		{name: "same scope after clarification", input: "Beta", prior: "Inspect the README in my project", response: `{"action":"dispatch","scope":"project","choice":2}`, project: "internal-beta"},
		{name: "model guesses an authorized project", input: "Fix the project", response: `{"action":"dispatch","scope":"project","choice":1}`, clarify: true},
		{name: "model substitutes another project", input: "Check Alpha", response: `{"action":"dispatch","scope":"project","choice":2}`, clarify: true},
		{name: "model substitutes for missing project", input: "Check Gamma", prior: "We worked in Alpha", response: `{"action":"dispatch","scope":"project","choice":1}`, clarify: true},
		{name: "model selects foreign choice", input: "Check Gamma", response: `{"action":"dispatch","scope":"project","choice":999}`, clarify: true},
		{name: "model chooses between mentioned projects", input: "Fix Alpha or Beta", response: `{"action":"dispatch","scope":"project","choice":1}`, clarify: true},
		{name: "explicitly excluded project cannot grant scope", input: "不要在 Alpha 项目里改", response: `{"action":"dispatch","scope":"project","choice":1}`, clarify: true},
		{name: "project exclusion does not block following sentence", input: "Do not use Alpha. Use Beta for this work.", response: `{"action":"dispatch","scope":"project","choice":2}`, project: "internal-beta"},
		{name: "longer literal name wins for nested project names", input: "请检查示例站后台的 README", response: `{"action":"dispatch","scope":"project","choice":5}`, project: "site-admin"},
		{name: "nested names remain ambiguous when separately mentioned", input: "请检查示例站和示例站后台", response: `{"action":"dispatch","scope":"project","choice":5}`, clarify: true},
		{name: "named project cannot fall back to managed", input: "Fix Alpha", response: `{"action":"dispatch","scope":"managed"}`, clarify: true},
		{name: "project with unknown name cannot fall back to managed", input: "Fix project Gamma", response: `{"action":"dispatch","scope":"managed"}`, clarify: true},
		{name: "user provides a quoted absolute directory", input: "Work in `" + directory + "`", response: `{"action":"dispatch","scope":"directory"}`, directory: directory},
		{name: "typographic quotes preserve the entire explicit directory", input: "在“" + directory + "”里检查", response: `{"action":"dispatch","scope":"directory"}`, directory: directory},
		{name: "output after working does not disqualify an explicit scope", input: "之前的示例说错了。在 " + base + " 中读取后输出摘要。", response: `{"action":"dispatch","scope":"directory"}`, directory: base},
		{name: "user provides a literal absolute directory", input: "Work in " + base + ".", response: `{"action":"dispatch","scope":"directory"}`, directory: base},
		{name: "standalone choice answers a prior scope question", input: "`" + directory + "`", prior: "Inspect the README", response: `{"action":"dispatch","scope":"directory","continuePrevious":true}`, directory: directory},
		{name: "a negated directory cannot grant scope", input: "Do not work in " + base, response: `{"action":"dispatch","scope":"directory"}`, clarify: true},
		{name: "an example path cannot grant scope", input: "For example, you could work in " + base, response: `{"action":"dispatch","scope":"directory"}`, clarify: true},
		{name: "a path with a conflicting directory option cannot grant scope", input: "Could we work in " + base + " or in the default folder?", response: `{"action":"dispatch","scope":"directory"}`, clarify: true},
		{name: "a conditional directory cannot grant scope", input: "If needed, work in " + base, response: `{"action":"dispatch","scope":"directory"}`, clarify: true},
		{name: "an output destination cannot grant scope", input: "Save the output in " + base, response: `{"action":"dispatch","scope":"directory"}`, clarify: true},
		{name: "a path listed as context cannot grant scope", input: "These directories are examples: " + base, response: `{"action":"dispatch","scope":"directory"}`, clarify: true},
		{name: "a path alone without a prior scope question cannot grant scope", input: base, response: `{"action":"dispatch","scope":"directory"}`, clarify: true},
		{name: "URLs do not authorize absolute directories", input: "Read https://example.com" + base, response: `{"action":"dispatch","scope":"directory"}`, clarify: true},
		{name: "relative directories do not authorize absolute directories", input: "Work in ./project", response: `{"action":"dispatch","scope":"directory"}`, clarify: true},
		{name: "relative paths cannot fall back to managed", input: "检查路径 ~/work", response: `{"action":"dispatch","scope":"managed"}`, clarify: true},
		{name: "two directories are ambiguous", input: "Work in " + filepath.Join(base, "a") + " or " + filepath.Join(base, "b"), response: `{"action":"dispatch","scope":"directory"}`, clarify: true},
		{name: "directory does not become managed scope", input: "Work in " + base, response: `{"action":"dispatch","scope":"managed"}`, clarify: true},
		{name: "model cannot emit a new directory", input: "Fix the project", response: `{"action":"dispatch","scope":"directory","workingDirectory":"/tmp/other"}`, invalid: true},
		{name: "model can choose ordinary managed work without project evidence", input: "Search the web for Go's history", response: `{"action":"dispatch","scope":"managed"}`},
		{name: "an unrelated managed task may follow a clarification", input: "Search the web for Go's history", prior: "Inspect my project", response: `{"action":"dispatch","scope":"managed"}`},
		{name: "a clarification answer cannot continue project work in managed scope", input: "Whichever one", prior: "Inspect my project", response: `{"action":"dispatch","scope":"managed","continuePrevious":true}`, invalid: true},
	}
	projects := []models.Project{
		{ID: "internal-alpha", Name: "Alpha"}, {ID: "internal-beta", Name: "Beta"}, {ID: "other", Name: "Other"},
		{ID: "site", Name: "示例站"}, {ID: "site-admin", Name: "示例站后台"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := RouteProject(context.Background(), func(context.Context, string, string) (string, error) {
				return c.response, nil
			}, ProjectRoutingInput{Content: c.input, PreviousRequest: c.prior, Projects: projects})
			if c.invalid {
				if !errors.Is(err, status_error.RuntimeProtocolIncompatible) {
					t.Fatalf("expected invalid model response, got %v", err)
				}
				return
			}
			if err != nil || got.ProjectID != c.project || got.WorkingDirectory != c.directory || got.Clarification != c.clarify || (got.Message != "") != c.clarify {
				t.Fatalf("got %+v, error %v", got, err)
			}
		})
	}
}

func TestProjectRouterIncompleteCatalogCannotAuthorizeAnAmbiguousName(t *testing.T) {
	decision, err := RouteProject(t.Context(), func(_ context.Context, _, input string) (string, error) {
		if !strings.Contains(input, `"projectsIncomplete":true`) {
			t.Fatal("partial result looked like the full authorized catalog")
		}
		return `{"action":"dispatch","scope":"project","choice":1}`, nil
	}, ProjectRoutingInput{Content: "Check project Alpha", ProjectsIncomplete: true, Projects: []models.Project{{ID: "one", Name: "Alpha"}}})
	if err != nil || !decision.Clarification || decision.ProjectID != "" {
		t.Fatalf("incomplete project catalog granted a scope: %+v %v", decision, err)
	}
}

func TestProjectRouterRepliesToUnsupportedReminderWithoutDispatching(t *testing.T) {
	for _, tc := range []struct {
		// input 是这轮用户发言。
		input string
		// previous 是旧版已要求补充时间的提醒请求。
		previous string
		// want 是主会话应显示的能力说明。
		want string
	}{
		{input: "明天提醒我 看下新闻", want: "主会话目前还不能设置定时提醒。"},
		{input: "9点", previous: "明天提醒我 看下新闻", want: "主会话目前还不能设置定时提醒。"},
		{input: "9 am", previous: "Remind me tomorrow to check the news", want: "Scheduled reminders from the main conversation are not available yet."},
	} {
		t.Run(tc.input, func(t *testing.T) {
			called := false
			got, err := RouteProject(t.Context(), func(context.Context, string, string) (string, error) {
				called = true
				return `{"action":"dispatch","scope":"managed","continuePrevious":true}`, nil
			}, ProjectRoutingInput{Content: tc.input, PreviousRequest: tc.previous})
			if err != nil || !reflect.DeepEqual(got, ProjectRoute{Message: tc.want}) || called {
				t.Fatalf("expected only a direct reply about reminders, got %+v, model called = %v, error %v", got, called, err)
			}
		})
	}
	got, err := RouteProject(t.Context(), func(context.Context, string, string) (string, error) {
		return `{"action":"dispatch","scope":"managed"}`, nil
	}, ProjectRoutingInput{Content: "现在帮我查今天的新闻", PreviousRequest: "明天提醒我 看下新闻"})
	if err != nil || !reflect.DeepEqual(got, ProjectRoute{}) {
		t.Fatalf("a new immediate request after a reminder should still dispatch: %+v, %v", got, err)
	}
}

func TestProjectRouterChecksAllNamesBeforeBoundingModelCandidates(t *testing.T) {
	projects := make([]models.Project, 32, 34)
	for i := range projects {
		projects[i] = models.Project{ID: fmt.Sprintf("other-%d", i), Name: fmt.Sprintf("Other %d", i)}
	}
	projects = append(projects, models.Project{ID: "old-project", Name: "Old Project"})
	model := func(_ context.Context, _, data string) (string, error) {
		var payload struct {
			// Candidates 是实际发送给模型的有界候选目录。
			Candidates []projectRouteCandidate `json:"candidates"`
			// Incomplete 表示模型目录省略了没有出现在任务里的项目。
			Incomplete bool `json:"projectsIncomplete"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Candidates) != 32 || payload.Candidates[0].Name != "Old Project" || payload.Candidates[0].Choice != 33 || !payload.Incomplete {
			t.Fatalf("old project was not prioritized in the bounded candidate set: %+v", payload)
		}
		return `{"action":"dispatch","scope":"project","choice":33}`, nil
	}
	input := ProjectRoutingInput{Content: "Inspect the README in Old Project", Projects: projects}
	got, err := RouteProject(t.Context(), model, input)
	if err != nil || got.ProjectID != "old-project" || got.Clarification {
		t.Fatalf("exact old project could not dispatch: %+v %v", got, err)
	}
	input.Projects = append(input.Projects, models.Project{ID: "case-collision", Name: "OLD PROJECT"})
	got, err = RouteProject(t.Context(), model, input)
	if err != nil || got.ProjectID != "" || !got.Clarification {
		t.Fatalf("case collision outside the original model window granted scope: %+v %v", got, err)
	}
}

func TestProjectRouterMasksLiteralPathsAcrossModelContext(t *testing.T) {
	privateDirectory := filepath.Join(t.TempDir(), "private-data")
	decision, err := RouteProject(t.Context(), func(_ context.Context, _, input string) (string, error) {
		if strings.Contains(input, privateDirectory) || strings.Count(input, "[absolute path]") < 4 {
			t.Fatalf("private directory reached the Router: %s", input)
		}
		return `{"action":"dispatch","scope":"directory"}`, nil
	}, ProjectRoutingInput{
		Content:         "Work in `" + privateDirectory + "`",
		PreviousRequest: "Check the logs in `" + privateDirectory + "`",
		History:         []models.MainConversationMessage{{Role: "assistant", Content: "Use `" + privateDirectory + "`?"}},
		Quote:           models.MainConversationMessage{ID: "old", Role: "assistant", Content: "Directory `" + privateDirectory + "`"},
	})
	if err != nil || decision.WorkingDirectory != privateDirectory {
		t.Fatalf("masking lost the user's authorized Worker directory: %+v %v", decision, err)
	}
}

func TestProjectRouterRejectsOversizedUnscopedInputBeforeCallingTheModel(t *testing.T) {
	_, err := RouteProject(t.Context(), func(_ context.Context, _, _ string) (string, error) {
		t.Fatal("oversized input reached the model")
		return "", nil
	}, ProjectRoutingInput{Content: strings.Repeat("内容", 64*1024)})
	if !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("expected invalid routing input, got %v", err)
	}
}

func TestProjectRouterSimpleResponseUsesPublicContextWithoutWorkerScope(t *testing.T) {
	decision, err := RouteProject(context.Background(), func(_ context.Context, _, input string) (string, error) {
		if strings.Contains(input, "private-waker-id") || strings.Contains(input, "private-project-id") || strings.Contains(input, "private-source-id") {
			t.Fatalf("private identities reached the route model: %s", input)
		}
		var payload struct {
			// Content 是用户本次输入。
			Content string `json:"content"`
			// Partner 是伙伴的公开档案。
			Partner map[string]string `json:"partner"`
			// History 是已有公开文本。
			History []map[string]string `json:"history"`
		}
		if err := json.Unmarshal([]byte(input), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Content != "你好" || payload.Partner["name"] != "晨星" || payload.Partner["role"] != "项目助手" || len(payload.History) != 1 || payload.History[0]["content"] != "你好！" {
			t.Fatalf("missing router context: %+v", payload)
		}
		return "```json\n{\"action\":\"answer\",\"message\":\"你好，我是晨星。\"}\n```", nil
	}, ProjectRoutingInput{
		Content:  "你好",
		Partner:  models.Waker{ID: "private-waker-id", Name: "晨星", RoleName: "项目助手"},
		Projects: []models.Project{{ID: "private-project-id", Name: "Alpha"}},
		History:  []models.MainConversationMessage{{Role: "assistant", Content: "你好！", SourceChatID: "private-source-id"}},
	})
	if err != nil || decision.Message != "你好，我是晨星。" || decision.Clarification || decision.ProjectID != "" || decision.WorkingDirectory != "" {
		t.Fatalf("got %+v, error %v", decision, err)
	}
}

func TestRouterTurnInputKeepsCurrentPublicWorkAndExplicitQuoteWithoutReplayingDialogue(t *testing.T) {
	_, err := RouteProject(t.Context(), func(_ context.Context, _, fresh string) (string, error) {
		if !strings.Contains(fresh, "earlier direct answer") || !strings.Contains(fresh, "previous project report") {
			t.Fatalf("fresh route lost seed history: %s", fresh)
		}
		warm := RouterTurnInput(fresh)
		for _, token := range []string{"current request", "explicit old quote", "new public report", "returned ticket report"} {
			if !strings.Contains(warm, token) {
				t.Errorf("continued route lost %q: %s", token, warm)
			}
		}
		for _, token := range []string{"earlier direct answer", "previous project report", "private source", "removed work update"} {
			if strings.Contains(warm, token) {
				t.Errorf("continued route replayed %q: %s", token, warm)
			}
		}
		return `{"action":"answer","message":"read"}`, nil
	}, ProjectRoutingInput{
		Content: "current request",
		Quote:   models.MainConversationMessage{ID: "old", Role: "assistant", Content: "explicit old quote"},
		History: []models.MainConversationMessage{
			{Role: "assistant", Content: "previous project report", SourceChatID: "private source"},
			{Role: "assistant", Content: "earlier direct answer"},
			{Role: "assistant", Content: "new public report", SourceChatID: "private source"},
			{Role: "assistant", Content: "returned ticket report", OriginalWork: &models.MainConversationOriginalWork{ChatID: "private source"}},
			{Role: "assistant", Content: "removed work update", SourceChatID: "private source", RemovedAt: 3},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}
