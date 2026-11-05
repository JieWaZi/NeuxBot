// Package usage reports consumption from canonical provider terminal results.
package usage

import (
	"cmp"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	ledger "github.com/JieWaZi/NeuxBot/server/internal/usage"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

const snapshotLifetime = 20 * time.Minute

// Service rebuilds a disposable query projection from canonical native streams.
type Service struct {
	// store retains canonical native terminals independently of public chats.
	store *conversation.Store
	// projection manages rebuildable rows in the business database.
	projection *controllers.UsageController
	// reconcile lets one reader catch up the projection at a time.
	reconcile chan struct{}
	// key signs snapshots and cursors issued by this process.
	key [32]byte
}

func NewService(store *conversation.Store, projection *controllers.UsageController) *Service {
	service := &Service{store: store, projection: projection, reconcile: make(chan struct{}, 1)}
	rand.Read(service.key[:])
	return service
}

// Filter selects a half-open time range and optional admission attribution.
type Filter struct {
	// From 是包含的 Unix 毫秒下界。
	From int64 `json:"from"`
	// To 是排除的 Unix 毫秒上界。
	To int64 `json:"to"`
	// Timezone 是自然日分组的 IANA 时区。
	Timezone string `json:"timezone"`
	// WakerIDs 是用于筛选的原始伙伴标识。
	WakerIDs []string `json:"wakerIds,omitempty"`
	// ChatID 是用于筛选的原始对话标识。
	ChatID string `json:"chatId,omitempty"`
	// Provider 是用于筛选的原生后端标识。
	Provider string `json:"provider,omitempty"`
	// AccountID 是用于筛选的原始宿主账号标识。
	AccountID string `json:"accountId,omitempty"`
	// Source 是用于筛选的调用入口。
	Source string `json:"source,omitempty"`
}

// Coverage describes which canonical evidence was available.
type Coverage struct {
	// State reports whether coverage has known gaps.
	State string `json:"state"`
	// EarliestRetainedAt is the oldest retained terminal commit time in this snapshot.
	EarliestRetainedAt *int64 `json:"earliestRetainedAt"`
	// LegacyBackgroundUnknown marks the gap before canonical terminal collection.
	LegacyBackgroundUnknown bool `json:"legacyBackgroundUnknown"`
	// IndeterminateInvocations counts dispatched work without terminal evidence when known.
	IndeterminateInvocations *int64 `json:"indeterminateInvocations"`
	// Reasons lists structured coverage gaps.
	Reasons []string `json:"reasons"`
}

// Summary reports exact known subtotals for a snapshot.
type Summary struct {
	// Range is the applied filter.
	Range Filter `json:"range"`
	// Snapshot fixes the projection boundary for subsequent grouped reads.
	Snapshot string `json:"snapshot"`
	// Coverage describes retained native terminal evidence.
	Coverage Coverage `json:"coverage"`
	// Metrics reports token and invocation counts for the selected range.
	Metrics ledger.TokenMetrics `json:"metrics"`
}

// Group contains usage for one group in this snapshot.
type Group struct {
	// Key 是分组标识，空值表示归属未知。
	Key string `json:"key"`
	// Provider 是模型分组对应的原生后端。
	Provider string `json:"provider,omitempty"`
	// Metrics reports token and invocation counts for the group.
	Metrics ledger.TokenMetrics `json:"metrics"`
}

// GroupsPage returns the current groups and exact total group count.
type GroupsPage struct {
	// Summary contains totals for the full snapshot.
	Summary
	// Groups is the current group page.
	Groups []Group `json:"groups"`
	// TotalGroups counts all groups in the same filter.
	TotalGroups int64 `json:"totalGroups"`
	// NextCursor is the signed anchor for the next page.
	NextCursor string `json:"nextCursor,omitempty"`
	// HasMore reports whether another group page exists.
	HasMore bool `json:"hasMore"`
}

type snapshot struct {
	// Generation identifies the projection before a rebuild.
	Generation string `json:"g"`
	// HighWater fixes the last visible projection row.
	HighWater int64 `json:"w"`
	// Filter binds the snapshot to the original scope.
	Filter [32]byte `json:"f"`
	// ExpiresAt is the snapshot expiration time.
	ExpiresAt int64 `json:"e"`
	// EarliestAt fixes the oldest retained time observed by this snapshot.
	EarliestAt *int64 `json:"a"`
	// ProjectionPartial records whether canonical catch-up failed for this snapshot.
	ProjectionPartial bool `json:"p"`
}

type groupCursor struct {
	// Snapshot 将游标绑定到指定快照。
	Snapshot [32]byte `json:"s"`
	// Group 将游标绑定到分组维度。
	Group string `json:"g"`
	// Day 是自然日分组上一页的最后一天。
	Day string `json:"d,omitempty"`
	// RowID 是数据库分组上一页最后一组的快照行序号。
	RowID int64 `json:"r,omitempty"`
}

// Summary reads exact range totals at one projection boundary.
func (s *Service) Summary(ctx context.Context, filter Filter, token string) (Summary, error) {
	filter, _, err := validateFilter(filter)
	if err != nil {
		return Summary{}, err
	}
	view, token, err := s.resolveSnapshot(ctx, filter, token)
	if err != nil {
		return Summary{}, err
	}
	totals := &accumulator{}
	if err := s.projection.Walk(ctx, query(filter, view), totals.add); err != nil {
		return Summary{}, err
	}
	if err := s.checkGeneration(ctx, view); err != nil {
		return Summary{}, err
	}
	return finishSummary(filter, token, view, totals)
}

// Groups uses the same snapshot and seeks in key order.
func (s *Service) Groups(ctx context.Context, filter Filter, token, group, cursor string, limit int) (GroupsPage, error) {
	if group != "day" && group != "model" && group != "waker" && group != "provider" || limit < 0 || limit > 100 || token == "" {
		return GroupsPage{}, status_error.BadRequest
	}
	if limit == 0 {
		limit = 50
	}
	filter, loc, err := validateFilter(filter)
	if err != nil {
		return GroupsPage{}, err
	}
	view, token, err := s.resolveSnapshot(ctx, filter, token)
	if err != nil {
		return GroupsPage{}, err
	}
	digest := sha256.Sum256([]byte(token))
	var after groupCursor
	if cursor != "" {
		if !s.decode("groups", cursor, &after) || after.Group != group || after.Snapshot != digest ||
			group == "day" && after.Day == "" || group != "day" && after.RowID <= 0 {
			return GroupsPage{}, status_error.BadRequest
		}
	}
	scope := query(filter, view)
	totals := &accumulator{}
	groups := make(map[controllers.UsageGroupKey]*accumulator)
	var keys []controllers.UsageGroupPageKey
	var count int64
	if group != "day" {
		count, keys, err = s.projection.GroupKeys(ctx, scope, group, after.RowID, limit+1)
		if err != nil {
			return GroupsPage{}, err
		}
		for _, key := range keys[:min(len(keys), limit)] {
			groups[key.UsageGroupKey] = &accumulator{}
		}
	}
	err = s.projection.Walk(ctx, scope, func(row models.UsageRecord) error {
		if err := totals.add(row); err != nil {
			return err
		}
		var key controllers.UsageGroupKey
		switch group {
		case "day":
			key.Key = time.UnixMilli(row.RecordedAt).In(loc).Format(time.DateOnly)
			if groups[key] == nil {
				groups[key] = &accumulator{}
				keys = append(keys, controllers.UsageGroupPageKey{UsageGroupKey: key})
			}
		case "model":
			return nil
		case "waker":
			key.Key = row.WakerID
		case "provider":
			key.Key = row.Provider
		}
		if accumulator := groups[key]; accumulator != nil {
			return accumulator.add(row)
		}
		return nil
	})
	if err != nil {
		return GroupsPage{}, err
	}
	if group == "model" {
		if err := s.projection.WalkModels(ctx, scope, keys[:min(len(keys), limit)], func(row models.UsageRecord) error {
			return groups[controllers.UsageGroupKey{Key: row.Model, Provider: row.Provider}].add(row)
		}); err != nil {
			return GroupsPage{}, err
		}
	}
	if group == "day" {
		slices.SortFunc(keys, func(a, b controllers.UsageGroupPageKey) int { return cmp.Compare(a.Key, b.Key) })
		count = int64(len(keys))
		if after.Day != "" {
			at, found := slices.BinarySearchFunc(keys, after.Day, func(key controllers.UsageGroupPageKey, day string) int {
				return cmp.Compare(key.Key, day)
			})
			if !found {
				return GroupsPage{}, status_error.BadRequest
			}
			keys = keys[at+1:]
		}
	}
	summary, err := finishSummary(filter, token, view, totals)
	if err != nil {
		return GroupsPage{}, err
	}
	page := GroupsPage{Summary: summary, Groups: []Group{}, TotalGroups: count, HasMore: len(keys) > limit}
	keys = keys[:min(len(keys), limit)]
	for _, key := range keys {
		metrics, err := groups[key.UsageGroupKey].tokens.Metrics()
		if err != nil {
			return GroupsPage{}, err
		}
		page.Groups = append(page.Groups, Group{Key: key.Key, Provider: key.Provider, Metrics: metrics})
	}
	if page.HasMore {
		last := keys[len(keys)-1]
		anchor := groupCursor{Snapshot: digest, Group: group, RowID: last.RowID}
		if group == "day" {
			anchor.Day = last.Key
		}
		page.NextCursor = s.encode("groups", anchor)
	}
	if err := s.checkGeneration(ctx, view); err != nil {
		return GroupsPage{}, err
	}
	return page, nil
}

func validateFilter(filter Filter) (Filter, *time.Location, error) {
	if filter.From <= 0 || filter.To <= filter.From || filter.To-filter.From > int64(368*24*time.Hour/time.Millisecond) ||
		filter.Timezone == "" || len(filter.Timezone) > 80 || filter.Timezone == "Local" || len(filter.WakerIDs) > 100 ||
		len(filter.ChatID) > 128 || len(filter.AccountID) > 128 || len(filter.Source) > 32 ||
		len(filter.Provider) > 32 || strings.TrimSpace(filter.Provider) != filter.Provider {
		return Filter{}, nil, status_error.BadRequest
	}
	loc, err := time.LoadLocation(filter.Timezone)
	if err != nil {
		return Filter{}, nil, status_error.BadRequest
	}
	civil := func(at int64) time.Time {
		y, m, d := time.UnixMilli(at).In(loc).Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	from, to := civil(filter.From), civil(filter.To-1)
	if to.Sub(from) >= 366*24*time.Hour || to.Year() > 9999 {
		return Filter{}, nil, status_error.BadRequest
	}
	filter.WakerIDs = slices.Clone(filter.WakerIDs)
	for _, id := range filter.WakerIDs {
		if id == "" || len(id) > 128 || strings.TrimSpace(id) != id {
			return Filter{}, nil, status_error.BadRequest
		}
	}
	slices.Sort(filter.WakerIDs)
	filter.WakerIDs = slices.Compact(filter.WakerIDs)
	return filter, loc, nil
}

func query(filter Filter, view snapshot) controllers.UsageQuery {
	return controllers.UsageQuery{From: filter.From, To: filter.To, HighWater: view.HighWater, WakerIDs: filter.WakerIDs,
		ChatID: filter.ChatID, Provider: filter.Provider, AccountID: filter.AccountID, Source: filter.Source}
}

func (s *Service) resolveSnapshot(ctx context.Context, filter Filter, token string) (snapshot, string, error) {
	var value snapshot
	raw, _ := json.Marshal(filter)
	fingerprint := sha256.Sum256(raw)
	if token != "" {
		if !s.decode("snapshot", token, &value) || time.Now().UnixMilli() >= value.ExpiresAt {
			return snapshot{}, "", status_error.Conflict
		}
		if value.Filter != fingerprint {
			return snapshot{}, "", status_error.BadRequest
		}
		if err := s.checkGeneration(ctx, value); err != nil {
			return snapshot{}, "", err
		}
		return value, token, nil
	}
	select {
	case <-ctx.Done():
		return snapshot{}, "", ctx.Err()
	case s.reconcile <- struct{}{}:
		defer func() { <-s.reconcile }()
	}
	checkpoints, err := s.projection.Checkpoints(ctx)
	if err != nil {
		return snapshot{}, "", err
	}
	value.Generation, value.Filter, value.ExpiresAt = checkpoints[0].Generation, fingerprint, time.Now().Add(snapshotLifetime).UnixMilli()
	if err := s.catchUp(ctx, value.Generation, checkpoints[1:]); err != nil {
		if ctx.Err() != nil {
			return snapshot{}, "", ctx.Err()
		}
		if errors.Is(err, status_error.DatabaseError) || errors.Is(err, status_error.DatabaseBusy) {
			return snapshot{}, "", err
		}
		logx.Errorf("usage projection could not read canonical native stream: error=%v", err)
		value.ProjectionPartial = true
	}
	value.HighWater, value.EarliestAt, err = s.projection.UsageHighWater(ctx)
	if err != nil {
		return snapshot{}, "", err
	}
	return value, s.encode("snapshot", value), nil
}

func (s *Service) checkGeneration(ctx context.Context, view snapshot) error {
	generation, err := s.projection.Generation(ctx)
	if err != nil {
		return err
	}
	if generation != view.Generation {
		return status_error.Conflict
	}
	return nil
}

func (s *Service) catchUp(ctx context.Context, generation string, checkpoints []models.UsageCheckpoint) error {
	var after [conversation.UsageShards]int64
	for _, checkpoint := range checkpoints {
		if checkpoint.Shard >= 0 && checkpoint.Shard < len(after) {
			after[checkpoint.Shard] = checkpoint.Cursor
		}
	}
	var result error
	for shard, cursor := range after {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		through, err := s.store.UsageHighWater(ctx, shard)
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		if through < cursor {
			result = errors.Join(result, status_error.ChatLogCorrupted)
			continue
		}
		for cursor < through {
			terminals, next, err := s.store.ReadUsageTerminals(ctx, shard, cursor, through)
			if err != nil {
				result = errors.Join(result, err)
				break
			}
			records := make([]models.UsageRecord, 0, len(terminals))
			for _, terminal := range terminals {
				records = project(records, terminal)
			}
			if err := s.projection.Project(ctx, generation, shard, next, records); err != nil {
				return err
			}
			cursor = next
		}
	}
	return result
}

func project(records []models.UsageRecord, terminal conversation.UsageTerminal) []models.UsageRecord {
	result := terminal.Result
	digest := result.Key.Digest()
	row := models.UsageRecord{NativeID: digest[:], RecordedAt: terminal.RecordedAt, Provider: result.Key.Session.Provider}
	if owner := result.Attribution; owner != nil {
		row.ChatID, row.WakerID, row.AccountID, row.Source = owner.ChatID, owner.WakerID, owner.AccountID, owner.Source
	} else if result.Turn.Origin == "provider" && len(result.Turn.ConsumedInputIDs) == 0 {
		row.Source = "provider"
	}
	observed := result.Turn.Usage
	if observed == nil {
		return append(records, row)
	}
	row.Coverage, row.Complete = observed.Coverage, observed.Complete
	base := row
	row.Model = observed.Model
	applyTokens(&row, ledger.ReadTokens(observed))
	parent := len(records)
	records = append(records, row)
	for _, model := range slices.Sorted(maps.Keys(observed.Models)) {
		if model == "" {
			continue
		}
		tokens := ledger.ReadModelTokens(observed.Models[model])
		if tokens == (ledger.Tokens{}) {
			continue
		}
		detail := base
		applyTokens(&detail, tokens)
		modelDigest := sha256.Sum256(append(digest[:], model...))
		detail.NativeID, detail.Model, detail.Scope = modelDigest[:], model, models.UsageScopeModelOnly
		records = append(records, detail)
	}
	if len(records) > parent+1 {
		records[parent].Scope = models.UsageScopeInvocationOnly
	}
	return records
}

func applyTokens(row *models.UsageRecord, tokens ledger.Tokens) {
	row.Input, row.CacheRead, row.CacheWrite, row.Output, row.Reasoning = tokens.Input, tokens.CacheRead, tokens.CacheWrite, tokens.Output, tokens.Reasoning
}

func (s *Service) encode(kind string, value any) string {
	raw, _ := json.Marshal(value)
	mac := hmac.New(sha256.New, s.key[:])
	mac.Write([]byte(kind))
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(append(raw, mac.Sum(nil)...))
}

func (s *Service) decode(kind, token string, value any) bool {
	if len(token) > 2048 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) <= sha256.Size {
		return false
	}
	body, signature := raw[:len(raw)-sha256.Size], raw[len(raw)-sha256.Size:]
	mac := hmac.New(sha256.New, s.key[:])
	mac.Write([]byte(kind))
	mac.Write(body)
	return hmac.Equal(signature, mac.Sum(nil)) && json.Unmarshal(body, value) == nil
}
