package workspacefiles

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

const detachedGitBranch = "HEAD (no branch)"

var gitConventionalMainBranches = [...]string{"main", "master", "trunk"}

type gitRefState struct {
	// branch 是分支名或分离头标记。
	branch string
	// head 是当前提交标识，尚未提交时为空。
	head string
	// upstream 是跟踪分支短名。
	upstream string
	// index 是同一份 porcelain 快照的暂存指纹。
	index string
	// ahead 是当前分支独有的提交数。
	ahead int
	// behind 是上游分支独有的提交数。
	behind int
}

func gitRefs(ctx context.Context, repo string) (gitRefState, error) {
	output, err := gitCommand(ctx, repo, "status", "--porcelain=v2", "-z", "--branch", "--ahead-behind", "--untracked-files=no")
	return parseGitRefs(output), err
}

func parseGitRefs(value string) gitRefState {
	result := gitRefState{index: gitIndexToken(value)}
	for rest := value; strings.HasPrefix(rest, "# "); {
		var header string
		header, rest, _ = strings.Cut(rest, "\x00")
		key, value, _ := strings.Cut(header[2:], " ")
		switch key {
		case "branch.head":
			result.branch = value
			if value == "(detached)" {
				result.branch = detachedGitBranch
			}
		case "branch.oid":
			if value != "(initial)" {
				result.head = value
			}
		case "branch.upstream":
			result.upstream = value
		case "branch.ab":
			ahead, behind, _ := strings.Cut(value, " ")
			result.ahead, _ = strconv.Atoi(strings.TrimPrefix(ahead, "+"))
			result.behind, _ = strconv.Atoi(strings.TrimPrefix(behind, "-"))
		}
	}
	return result
}

func gitIndexToken(porcelain string) string {
	digest := sha256.New()
	staged := false
	for rest := porcelain; rest != ""; {
		var entry, oldPath string
		entry, rest, _ = strings.Cut(rest, "\x00")
		if strings.HasPrefix(entry, "2 ") {
			oldPath, rest, _ = strings.Cut(rest, "\x00")
		}
		if len(entry) < 4 || entry[1] != ' ' || entry[2] == '.' {
			continue
		}
		var count int
		switch entry[0] {
		case '1':
			count = 9
		case '2':
			count = 10
		case 'u':
			count = 11
		default:
			continue
		}
		fields := strings.SplitN(entry, " ", count)
		if len(fields) != count {
			continue
		}
		// 双列状态中的首列、暂存模式、对象标识与路径共同确定暂存内容。
		// 文件模式、状态及路径都不包含分隔符。
		identity := []string{entry[:1], entry[2:3], fields[3], fields[4], fields[6], fields[7], fields[len(fields)-1], oldPath}
		if entry[0] == 'u' {
			identity = append(identity, fields[5], fields[8], fields[9])
		}
		digest.Write([]byte(strings.Join(identity, "\x00") + "\x00"))
		staged = true
	}
	if !staged {
		return ""
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func gitRemoteNames(ctx context.Context, repo string) ([]string, error) {
	output, err := gitCommand(ctx, repo, "remote")
	return strings.Fields(output), err
}

func gitUpstreamDestination(ctx context.Context, repo, branch string) (string, string, error) {
	if branch == "" || branch == detachedGitBranch {
		return "", "", nil
	}
	output, err := gitCommand(ctx, repo, "for-each-ref", "--format=%(refname)%00%(upstream:remotename)%00%(upstream:remoteref)", "refs/heads/"+branch)
	if err != nil {
		return "", "", err
	}
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Split(line, "\x00")
		if len(fields) == 3 && fields[0] == "refs/heads/"+branch {
			return fields[1], strings.TrimPrefix(fields[2], "refs/heads/"), nil
		}
	}
	return "", "", nil
}

func preferredGitRemote(ctx context.Context, repo string, refs gitRefState, remotes []string) (string, error) {
	if refs.branch == "" || refs.branch == detachedGitBranch || len(remotes) == 0 {
		return "", nil
	}
	upstream, _, err := gitUpstreamDestination(ctx, repo, refs.branch)
	if err != nil {
		return "", err
	}
	var origin string
	for _, remote := range remotes {
		if remote == upstream {
			return remote, nil
		}
		if remote == "origin" {
			origin = remote
		}
	}
	if len(remotes) == 1 {
		return remotes[0], nil
	}
	return origin, nil
}

func gitKnownDefaultBranch(ctx context.Context, repo, remote string) (string, error) {
	remotePrefix := "refs/remotes/" + remote + "/"
	symbolicHead := remotePrefix + "HEAD"
	args := []string{"for-each-ref", "--format=%(refname)%00%(symref)", symbolicHead}
	for _, branch := range gitConventionalMainBranches {
		args = append(args, remotePrefix+branch)
	}
	output, err := gitCommand(ctx, repo, args...)
	if err != nil {
		return "", err
	}
	known := make(map[string]bool, len(gitConventionalMainBranches))
	for line := range strings.SplitSeq(output, "\n") {
		ref, target, _ := strings.Cut(line, "\x00")
		if ref == symbolicHead {
			if branch, ok := strings.CutPrefix(target, remotePrefix); ok {
				return branch, nil
			}
		} else if branch, ok := strings.CutPrefix(ref, remotePrefix); ok {
			known[branch] = true
		}
	}
	for _, branch := range gitConventionalMainBranches {
		if known[branch] {
			return branch, nil
		}
	}
	return "", nil
}
