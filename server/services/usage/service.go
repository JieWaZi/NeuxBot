// Package usage reports consumption from canonical provider terminal results.
package usage

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog_next"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	ledger "github.com/JieWaZi/NeuxBot/server/internal/usage"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

const snapshotLifetime = 20 * time.Minute

// Service rebuilds a disposable query projection from canonical native streams.
type Service struct {
	// store retains canonical native terminals independently of public chats.
	store *eventlog_next.Store
	// projection manages rebuildable rows in the business database.
	projection *controllers.UsageController
	// reconcile lets one reader catch up the projection at a time.
	reconcile chan struct{}
	// key signs snapshots and cursors issued by this process.
	key [32]byte
}

func NewService(store *eventlog_next.Store, projection *controllers.UsageController) *Service {
	service := &Service{store: store, projection: projection, reconcile: make(chan struct{}, 1)}
	rand.Read(service.key[:])
	return service
}

// Filter selects a half-open time range and optional admission attribution.
type Filter struct {
	// From is the inclusive Unix millisecond bound.
	From int64 `json:"from"`
	// To is the exclusive Unix millisecond bound.
	To int64 `json:"to"`
	// Timezone is the IANA zone for calendar-day grouping.
	Timezone string `json:"timezone"`
	// WakerIDs optionally selects original partners.
	WakerIDs []string `json:"wakerIds,omitempty"`
	// ChatID optionally selects the original conversation.
	ChatID string `json:"chatId,omitempty"`
	// AccountID optionally selects the original host account.
	AccountID string `json:"accountId,omitempty"`
	// Source optionally selects the host entry point.
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
	// Metrics contains token usage and valuations for the filter.
	Metrics Metrics `json:"metrics"`
}

// Group contains usage for a day, model, or partner in this snapshot.
type Group struct {
	// Key is the grouping identity. An empty key means attribution is unknown.
	Key string `json:"key"`
	// Metrics contains usage and valuations for this group.
	Metrics Metrics `json:"metrics"`
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
	// Snapshot binds the cursor to the exact snapshot.
	Snapshot [32]byte `json:"s"`
	// Group binds the cursor to the grouping dimension.
	Group string `json:"g"`
	// Last is the final key of the preceding page.
	Last string `json:"l"`
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
	totals := newAccumulator()
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
	if group != "day" && group != "model" && group != "waker" || limit < 0 || limit > 100 || token == "" {
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
	var after *string
	if cursor != "" {
		var anchor groupCursor
		if !s.decode("groups", cursor, &anchor) || anchor.Group != group || anchor.Snapshot != digest {
			return GroupsPage{}, status_error.BadRequest
		}
		after = &anchor.Last
	}
	scope := query(filter, view)
	totals := newAccumulator()
	groups := make(map[string]*accumulator)
	var keys []string
	var count int64
	if group != "day" {
		count, keys, err = s.projection.GroupKeys(ctx, scope, group, after, limit+1)
		if err != nil {
			return GroupsPage{}, err
		}
		for _, key := range keys[:min(len(keys), limit)] {
			groups[key] = newAccumulator()
		}
	}
	err = s.projection.Walk(ctx, scope, func(row models.UsageRecord) error {
		if err := totals.add(row); err != nil {
			return err
		}
		var key string
		switch group {
		case "day":
			key = time.UnixMilli(row.RecordedAt).In(loc).Format(time.DateOnly)
			if groups[key] == nil {
				groups[key] = newAccumulator()
				keys = append(keys, key)
			}
		case "model":
			key = row.Model
		case "waker":
			key = row.WakerID
		}
		if accumulator := groups[key]; accumulator != nil {
			return accumulator.add(row)
		}
		return nil
	})
	if err != nil {
		return GroupsPage{}, err
	}
	if group == "day" {
		slices.Sort(keys)
		count = int64(len(keys))
		if after != nil {
			at, found := slices.BinarySearch(keys, *after)
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
		metrics, err := groups[key].metrics()
		if err != nil {
			return GroupsPage{}, err
		}
		page.Groups = append(page.Groups, Group{Key: key, Metrics: metrics})
	}
	if page.HasMore {
		page.NextCursor = s.encode("groups", groupCursor{Snapshot: digest, Group: group, Last: keys[len(keys)-1]})
	}
	if err := s.checkGeneration(ctx, view); err != nil {
		return GroupsPage{}, err
	}
	return page, nil
}

func validateFilter(filter Filter) (Filter, *time.Location, error) {
	if filter.From <= 0 || filter.To <= filter.From || filter.To-filter.From > int64(368*24*time.Hour/time.Millisecond) ||
		filter.Timezone == "" || len(filter.Timezone) > 80 || filter.Timezone == "Local" || len(filter.WakerIDs) > 100 ||
		len(filter.ChatID) > 128 || len(filter.AccountID) > 128 || len(filter.Source) > 32 {
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
		ChatID: filter.ChatID, AccountID: filter.AccountID, Source: filter.Source}
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
	var after [eventlog_next.UsageShards]int64
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
				records = append(records, project(terminal))
			}
			if err := s.projection.Project(ctx, generation, shard, next, records); err != nil {
				return err
			}
			cursor = next
		}
	}
	return result
}

func project(terminal eventlog_next.UsageTerminal) models.UsageRecord {
	result := terminal.Result
	digest := result.Key.Digest()
	row := models.UsageRecord{NativeID: digest[:], RecordedAt: terminal.RecordedAt}
	if owner := result.Attribution; owner != nil {
		row.ChatID, row.WakerID, row.AccountID, row.Source = owner.ChatID, owner.WakerID, owner.AccountID, owner.Source
	} else if result.Turn.Origin == "provider" && len(result.Turn.ConsumedInputIDs) == 0 {
		row.Source = "provider"
	}
	if observed := result.Turn.Usage; observed != nil {
		tokens := ledger.ReadTokens(observed)
		row.Input, row.CacheRead, row.CacheWrite, row.Output, row.Reasoning = tokens.Input, tokens.CacheRead, tokens.CacheWrite, tokens.Output, tokens.Reasoning
		row.Model, row.Coverage, row.Complete = observed.Model, observed.Coverage, observed.Complete
		if len(observed.Models) > 1 {
			row.Model = ""
		}
		row.Amount, row.CostComplete = ledger.NativeAmount(observed.Cost)
		if row.Amount != "" {
			row.Currency, row.Valuation, row.CostSource, row.CostOrigin = observed.Cost.Currency, observed.Cost.Valuation, observed.Cost.Source, "native"
		}
	}
	return row
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
