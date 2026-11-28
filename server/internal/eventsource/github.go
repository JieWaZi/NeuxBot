package eventsource

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

const gitHubBufferLimit = 2 * 1024 * 1024

var githubRepoName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,38}/[a-z0-9_.-]{1,100}$`)

func gitHubKinds() []string { return []string{PullOpened, PullReviewed, ChecksPassed} }

func normalizeGitHub(value Config) (Config, error) {
	if value.Source != GitHubPullRequests {
		return Config{}, status_error.BadRequest
	}
	repository := strings.ToLower(strings.TrimSpace(value.Repository))
	if strings.Contains(repository, "://") {
		u, err := url.Parse(repository)
		if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return Config{}, status_error.BadRequest
		}
		repository = strings.TrimPrefix(u.Path, "/")
	}
	repository = strings.TrimSuffix(strings.TrimSuffix(repository, "/"), ".git")
	_, name, _ := strings.Cut(repository, "/")
	value.Keyword = strings.TrimSpace(value.Keyword)
	value.SemanticCondition = strings.TrimSpace(value.SemanticCondition)
	if !githubRepoName.MatchString(repository) || name == "." || name == ".." || !utf8.ValidString(value.Keyword) || utf8.RuneCountInString(value.Keyword) > 120 {
		return Config{}, status_error.BadRequest
	}
	if !utf8.ValidString(value.SemanticCondition) || utf8.RuneCountInString(value.SemanticCondition) > 500 {
		return Config{}, status_error.BadRequest
	}
	value.Repository = repository
	if len(value.Kinds) == 0 {
		value.Kinds = []string{PullOpened}
		return value, nil
	}
	kinds := gitHubKinds()
	if len(value.Kinds) > len(kinds) {
		return Config{}, status_error.BadRequest
	}
	for _, kind := range value.Kinds {
		if !slices.Contains(kinds, kind) {
			return Config{}, status_error.BadRequest
		}
	}
	selected := make([]string, 0, len(value.Kinds))
	for _, kind := range kinds {
		if slices.Contains(value.Kinds, kind) {
			selected = append(selected, kind)
		}
	}
	value.Kinds = selected
	return value, nil
}

// GitHubSource 使用已有 GitHub CLI 账号读取最近至多三十个 PR。
type GitHubSource struct {
	// ReadPulls 可替换 GitHub 返回值以便确定性验证。
	ReadPulls func(context.Context, string) (json.RawMessage, error)
}

type gitHubPull struct {
	// Number 是目标仓库中的 PR 编号。
	Number int `json:"number"`
	// Title 是该 PR 的外部标题。
	Title string `json:"title"`
	// Draft 表示该 PR 在 GitHub 中还是草稿。
	Draft bool `json:"isDraft"`
	// Head 标识本次检查所属的提交。
	Head string `json:"headRefOid"`
	// Reviews 保存该 PR 已可见的评审。
	Reviews []gitHubReview `json:"reviews"`
	// Checks 保存头提交的检查与传统状态。
	Checks []gitHubCheck `json:"statusCheckRollup"`
}

type gitHubReview struct {
	// ID 是 GitHub 提供的评审稳定身份。
	ID string `json:"id"`
	// State 区分已提交和仍待提交的评审。
	State string `json:"state"`
	// SubmittedAt 是已提交评审的实际时间。
	SubmittedAt string `json:"submittedAt"`
}

type gitHubCheck struct {
	// Status 是检查当前的生命周期状态。
	Status string `json:"status"`
	// Conclusion 是检查结束之后的结论。
	Conclusion string `json:"conclusion"`
	// State 是传统提交状态的结果。
	State string `json:"state"`
}

// Observe 将 GitHub 记录映射为可重放的只读观察。
func (s GitHubSource) Observe(ctx context.Context, config Config) ([]Event, error) {
	config, err := Normalize(config)
	if err != nil {
		return nil, err
	}
	read := s.ReadPulls
	if read == nil {
		read = readGitHubPulls
	}
	body, err := read(ctx, config.Repository)
	if err != nil {
		return nil, err
	}
	var pulls []gitHubPull
	if err := json.Unmarshal(body, &pulls); err != nil {
		return nil, status_error.WorkspaceGitHubRequestFailed
	}
	var events []Event
	for _, pull := range pulls {
		if pull.Number <= 0 {
			continue
		}
		topic := fmt.Sprintf("github:%s:pull:%d", config.Repository, pull.Number)
		title := []rune(strings.Join(strings.Fields(pull.Title), " "))
		if len(title) > 240 {
			title = title[:240]
		}
		base := Event{Topic: topic, Title: string(title), URL: fmt.Sprintf("https://github.com/%s/pull/%d", config.Repository, pull.Number), Draft: pull.Draft}
		if slices.Contains(config.Kinds, PullOpened) {
			event := base
			event.ID, event.Kind = topic+":opened", PullOpened
			events = append(events, event)
		}
		if slices.Contains(config.Kinds, PullReviewed) {
			for _, review := range pull.Reviews {
				if review.ID == "" || review.State == "PENDING" || review.SubmittedAt == "" {
					continue
				}
				event := base
				event.ID, event.Kind = fmt.Sprintf("%s:review:%x", topic, sha256.Sum256([]byte(review.ID))), PullReviewed
				events = append(events, event)
			}
		}
		if slices.Contains(config.Kinds, ChecksPassed) && pull.Head != "" && gitHubChecksPassed(pull.Checks) {
			event := base
			event.ID, event.Kind = fmt.Sprintf("%s:checks:%x", topic, sha256.Sum256([]byte(pull.Head))), ChecksPassed
			events = append(events, event)
		}
	}
	return events, nil
}

func gitHubChecksPassed(checks []gitHubCheck) bool {
	passed := false
	for _, check := range checks {
		switch {
		case check.State == "SUCCESS", check.Status == "COMPLETED" && check.Conclusion == "SUCCESS":
			passed = true
		case check.Status == "COMPLETED" && (check.Conclusion == "SKIPPED" || check.Conclusion == "NEUTRAL"):
		default:
			return false
		}
	}
	return passed
}

func readGitHubPulls(ctx context.Context, repository string) (json.RawMessage, error) {
	path, err := exec.LookPath("gh")
	if err != nil {
		return nil, status_error.WorkspaceGitHubCLIUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, path, "pr", "list", "--repo", "github.com/"+repository, "--state", "all", "--limit", "30", "--json", "number,title,isDraft,headRefOid,reviews,statusCheckRollup")
	command.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1", "GH_DEBUG=")
	output := &gitHubOutput{}
	command.Stdout = output
	if err := command.Run(); err != nil {
		return nil, status_error.WorkspaceGitHubRequestFailed
	}
	return output.value, nil
}

type gitHubOutput struct {
	// value 保存有明确大小上限的响应。
	value json.RawMessage
}

func (o *gitHubOutput) Write(p []byte) (int, error) {
	if len(p) > gitHubBufferLimit-len(o.value) {
		return 0, status_error.WorkspaceGitHubRequestFailed
	}
	o.value = append(o.value, p...)
	return len(p), nil
}
