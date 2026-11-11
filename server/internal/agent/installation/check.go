package installation

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"golang.org/x/mod/semver"
)

// Release is the publisher's current artifact identity, without downloading its payload.
type Release struct {
	// Version is the native CLI version declared by the official release metadata.
	Version string
	// SourceURL is the exact official artifact selected by that metadata.
	SourceURL string
}

// CheckVersion resolves only bounded official release metadata; it never downloads or executes a CLI.
func (i *Installer) CheckVersion(ctx context.Context, runtimeID string) (Release, error) {
	artifact, err := i.resolve(ctx, runtimeID)
	if err != nil {
		return Release{}, err
	}
	return Release{Version: artifact.version, SourceURL: artifact.url}, nil
}

var nativeVersionToken = regexp.MustCompile(`^(?:rust-)?v?[0-9]+\.[0-9]+(?:\.[0-9]+)?(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
var cursorVersionToken = regexp.MustCompile(`^([0-9]{4})\.([0-9]{2})\.([0-9]{2})(?:-([0-9A-Za-z.-]+))?$`)

// CompareVersions compares verified native versions. Unknown or unordered builds remain incomparable.
// The returned order is negative when local is older, zero when equal, and positive when newer.
func CompareVersions(runtimeID, local, latest string) (int, bool) {
	left, right := versionToken(local), versionToken(latest)
	if left == "" || right == "" {
		return 0, false
	}
	if runtimeID == constants.RuntimeIDCursor {
		a, b := cursorVersionToken.FindStringSubmatch(left), cursorVersionToken.FindStringSubmatch(right)
		if len(a) == 0 || len(b) == 0 {
			return 0, false
		}
		date := func(parts []string) (time.Time, bool) {
			year, _ := strconv.Atoi(parts[1])
			month, _ := strconv.Atoi(parts[2])
			day, _ := strconv.Atoi(parts[3])
			value := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
			return value, value.Year() == year && int(value.Month()) == month && value.Day() == day
		}
		x, validX := date(a)
		y, validY := date(b)
		if !validX || !validY {
			return 0, false
		}
		if x.Before(y) {
			return -1, true
		}
		if x.After(y) {
			return 1, true
		}
		// Publisher hashes do not encode an ordering within one release date.
		return 0, left == right
	}
	canonical := func(value string) string {
		value = strings.TrimPrefix(value, "rust-")
		if !strings.HasPrefix(value, "v") {
			value = "v" + value
		}
		return value
	}
	left, right = canonical(left), canonical(right)
	if !semver.IsValid(left) || !semver.IsValid(right) {
		return 0, false
	}
	return semver.Compare(left, right), true
}

func versionToken(value string) string {
	var result string
	for _, part := range strings.Fields(strings.TrimSpace(value)) {
		part = strings.Trim(part, "()[],:;")
		if !nativeVersionToken.MatchString(part) {
			continue
		}
		if result != "" && result != part {
			return ""
		}
		result = part
	}
	return result
}
