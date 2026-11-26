package workspacefiles

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

var githubRepositoryPath = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9_.-]+$`)

func githubPushRepository(ctx context.Context, repo, remote string) (string, error) {
	raw, err := gitCommand(ctx, repo, "remote", "get-url", "--push", "--all", remote)
	if err != nil {
		return "", err
	}
	value := strings.TrimSuffix(raw, "\n")
	path, scp := strings.CutPrefix(value, "git@github.com:")
	if !scp {
		parsed, err := url.Parse(value)
		if err != nil {
			return "", status_error.WorkspaceGitHubUnsupported
		}
		host := strings.ToLower(parsed.Hostname())
		if !(parsed.Scheme == "https" && host == "github.com" && (parsed.Port() == "" || parsed.Port() == "443") ||
			parsed.Scheme == "ssh" && (host == "github.com" || host == "ssh.github.com")) {
			return "", status_error.WorkspaceGitHubUnsupported
		}
		path = strings.TrimPrefix(parsed.Path, "/")
	}
	path = strings.TrimSuffix(strings.TrimSuffix(path, "/"), ".git")
	_, name, _ := strings.Cut(path, "/")
	if !githubRepositoryPath.MatchString(path) || name == "." || name == ".." {
		return "", status_error.WorkspaceGitHubUnsupported
	}
	return path, nil
}

type gitHubPR struct {
	// URL 是 GitHub 提供的网页地址。
	URL string `json:"html_url"`
}

type gitHubPRTarget struct {
	// repository 是远端提供方的 Owner/Repository 标识。
	repository string
	// base 是确认的目标分支。
	base string
}

func prepareGitHubPR(ctx context.Context, repo, remote, base string, findOnly bool) (gitHubPRTarget, error) {
	repository, err := githubPushRepository(ctx, repo, remote)
	if err != nil {
		return gitHubPRTarget{}, err
	}
	if base == "" && !findOnly {
		executable, err := exec.LookPath("gh")
		if err != nil {
			return gitHubPRTarget{}, status_error.WorkspaceGitHubCLIUnavailable
		}
		var info struct {
			// DefaultBranch 是 GitHub 仓库配置的默认目标。
			DefaultBranch string `json:"default_branch"`
		}
		if err = gitHubAPI(ctx, executable, "GET", "repos/"+repository, &info); err != nil {
			return gitHubPRTarget{}, err
		}
		if !validGitBranch(ctx, repo, info.DefaultBranch) {
			return gitHubPRTarget{}, status_error.WorkspaceGitHubRequestFailed
		}
		base = info.DefaultBranch
	}
	return gitHubPRTarget{repository: repository, base: base}, nil
}

func runGitHubPR(ctx context.Context, repo string, refs gitRefState, target gitRemoteTarget, pr gitHubPRTarget, findOnly bool) (string, error) {
	executable, err := exec.LookPath("gh")
	if err != nil {
		return "", status_error.WorkspaceGitHubCLIUnavailable
	}
	repository, base := pr.repository, pr.base
	owner, _, _ := strings.Cut(repository, "/")
	head := owner + ":" + target.branch
	endpoint := "repos/" + repository + "/pulls"
	find := func() (string, error) {
		var values []gitHubPR
		fields := []string{"--raw-field", "state=open", "--raw-field", "head=" + head, "--field", "per_page=2"}
		if base != "" {
			fields = append(fields, "--raw-field", "base="+base)
		}
		err := gitHubAPI(ctx, executable, "GET", endpoint, &values, fields...)
		if err != nil || len(values) == 0 {
			return "", err
		}
		if len(values) > 1 {
			return "", status_error.WorkspaceResourceConflict
		}
		return gitHubPRURL(values[0].URL, repository)
	}
	existing, err := find()
	if err != nil || existing != "" || findOnly {
		return existing, err
	}
	if _, err = checkedGitRefs(ctx, repo, GitActionRequest{ExpectedBranch: &refs.branch, ExpectedHead: &refs.head}); err != nil {
		return "", err
	}
	// 核对现有远端 HEAD，直接创建 PR 的路径不会偷偷推送或使用未确认的提交。
	var remoteRef struct {
		// Object 定位远端分支实际指向的提交。
		Object struct {
			// SHA 是远端分支的提交标识。
			SHA string `json:"sha"`
		} `json:"object"`
	}
	encodedBranch := strings.Split(target.branch, "/")
	for index, part := range encodedBranch {
		encodedBranch[index] = url.PathEscape(part)
	}
	if err = gitHubAPI(ctx, executable, "GET", "repos/"+repository+"/git/ref/heads/"+strings.Join(encodedBranch, "/"), &remoteRef); err != nil {
		return "", err
	}
	if remoteRef.Object.SHA != refs.head {
		return "", status_error.WorkspaceResourceConflict
	}
	subject, err := gitCommand(ctx, repo, "show", "-s", "--format=%s", refs.head, "--")
	if err != nil {
		return "", err
	}
	title := strings.TrimSpace(subject)
	if title == "" {
		title = target.branch
	}
	const titleLimit = 240
	if runes := []rune(title); len(runes) > titleLimit {
		title = string(runes[:titleLimit])
	}
	var created gitHubPR
	err = gitHubAPI(ctx, executable, "POST", endpoint, &created,
		"--raw-field", "head="+head, "--raw-field", "base="+base, "--raw-field", "title="+title)
	if err != nil {
		// 并发创建或响应丢失时从现有 PR 对账一次。
		if existing, findErr := find(); findErr == nil && existing != "" {
			return existing, nil
		}
		return "", err
	}
	return gitHubPRURL(created.URL, repository)
}

func gitHubPRURL(value, repository string) (string, error) {
	parsed, err := url.Parse(value)
	if err == nil && parsed.Scheme == "https" && parsed.Host == "github.com" && parsed.User == nil &&
		parsed.RawQuery == "" && parsed.Fragment == "" {
		pullPrefix := "/" + strings.ToLower(repository) + "/pull/"
		number, ok := strings.CutPrefix(strings.ToLower(parsed.Path), pullPrefix)
		if ok && number != "" && strings.Trim(number, "0123456789") == "" {
			return value, nil
		}
	}
	logx.Warnf("workspace github pull response had an unexpected URL")
	return "", status_error.WorkspaceGitHubRequestFailed
}

func gitHubAPI(ctx context.Context, executable, method, endpoint string, result any, fields ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	args := []string{"api", "--hostname", "github.com", "--method", method, endpoint}
	command := exec.CommandContext(ctx, executable, append(args, fields...)...)
	command.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1", "GH_DEBUG=")
	output := &boundedGitOutput{limit: 512 * 1024}
	command.Stdout = output
	if err := command.Run(); err != nil {
		logx.Warnf("workspace github request failed: method=%s error=%v", method, err)
		return status_error.WorkspaceGitHubRequestFailed
	}
	if err := json.Unmarshal(output.buffer.Bytes(), result); err != nil {
		logx.Warnf("workspace github response invalid: method=%s error=%v", method, err)
		return status_error.WorkspaceGitHubRequestFailed
	}
	return nil
}
