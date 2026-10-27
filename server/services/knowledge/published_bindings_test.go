package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	rules "github.com/JieWaZi/NeuxBot/server/internal/integration"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	integrationservice "github.com/JieWaZi/NeuxBot/server/services/integration"
	"github.com/JieWaZi/neux-knowledge/core"
)

func retryFixture(t *testing.T) (*Service, *core.Library, *integrationservice.Service, controllers.IntegrationKeyController, map[string]json.RawMessage) {
	t.Helper()
	s, lib, _, db := newServiceFixtureWithDB(t)
	ctx := context.Background()
	keys := controllers.NewIntegrationKeyController(db)
	authority := integrationservice.New(integrationservice.Config{Keys: keys})
	s.config.IntegrationGuard = authority.RestoreKnowledgeGuard
	s.config.IntegrationPublicationGuard = authority.RestoreKnowledgeGuards
	raws := map[string]json.RawMessage{}
	for _, id := range []string{"a", "b"} {
		_, _, err := keys.ClaimMutation(ctx, models.IntegrationKeyMutation{RequestID: id, Digest: id, KeyID: id, Action: "create"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = keys.CompleteMutation(ctx, id, id, models.IntegrationKey{ID: id, Name: id, Enabled: true, KeyHash: rules.HashKey(id), Grants: []models.IntegrationGrant{{ResourceKind: constants.IntegrationResourceKindKnowledge, ResourceIDs: []string{lib.ID}, Capabilities: []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve, constants.IntegrationCapabilityOrganize}}}}, 0)
		if err != nil {
			t.Fatal(err)
		}
		identity, _ := authority.Authenticate(ctx, id)
		auth, _ := authority.Authorize(ctx, identity, constants.IntegrationResourceKindKnowledge, lib.ID, []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve, constants.IntegrationCapabilityOrganize})
		raws[id], _ = json.Marshal(integrationservice.KnowledgeRecovery{Origin: "direct_api", Subject: id, Authorization: auth, Permission: core.PermissionSnapshot{PolicyVersion: lib.PolicyVersion, Operations: []string{"organize"}}})
	}
	return s, lib, authority, keys, raws
}

func TestPublishedKnowledgeDoesNotGrantInternalPermanentCleanup(t *testing.T) {
	s, _, authority, _, _ := retryFixture(t)
	ctx := context.Background()
	identity, err := authority.Authenticate(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := s.PublishedBindings(ctx, authority, identity)
	if err != nil {
		t.Fatal(err)
	}
	for _, binding := range bindings {
		if binding.Operation == "knowledge.pages.purge" {
			t.Fatal("existing external Key silently gained permanent Wiki cleanup")
		}
	}
}
func acceptFailedJob(t *testing.T, s *Service, lib *core.Library, request string, source string, session models.KnowledgeTaskConfiguration) (*core.Job, OrganizeCommand) {
	t.Helper()
	ctx := context.Background()
	input := OrganizeCommand{RequestID: request, LibraryID: lib.ID, SourceVersionIDs: []string{source}, ExplicitSources: true, Configuration: session}
	r, _, e := s.Organize(ctx, input)
	if e != nil {
		t.Fatal(e)
	}
	j, _ := s.config.Engine.GetJob(ctx, r.JobID)
	if _, e = s.config.Engine.FailOrganize(ctx, j.ID, j.Attempt, "fixture failure"); e != nil {
		t.Fatal(e)
	}
	j, _ = s.config.Engine.GetJob(ctx, j.ID)
	return j, input
}
func revokeRetryKey(t *testing.T, authority *integrationservice.Service, keys controllers.IntegrationKeyController, id string) {
	t.Helper()
	ctx := context.Background()
	key, e := keys.Get(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	key.Enabled = false
	e = authority.WithAuthorizationWrite(ctx, func(ctx context.Context) error { _, e := keys.UpdateAuthorization(ctx, key, key.Revision); return e })
	if e != nil {
		t.Fatal(e)
	}
}

func TestFailedRetryAuthorityCandidateDoesNotPoisonNextCaller(t *testing.T) {
	for _, conflict := range []string{"version", "source"} {
		t.Run(conflict, func(t *testing.T) {
			s, lib, authority, keys, raws := retryFixture(t)
			ctx := context.Background()
			source, e := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "source", LibraryID: lib.ID, Filename: "source.md", Bytes: []byte("原文")})
			if e != nil {
				t.Fatal(e)
			}
			j, _ := acceptFailedJob(t, s, lib, "original", source.ResourceID, models.KnowledgeTaskConfiguration{})
			version := j.Version - 1
			var competing *core.Job
			if conflict == "source" {
				r, _, e := s.Organize(ctx, OrganizeCommand{RequestID: "competing", LibraryID: lib.ID, SourceVersionIDs: []string{source.ResourceID}, ExplicitSources: true})
				if e != nil {
					t.Fatal(e)
				}
				competing, _ = s.config.Engine.GetJob(ctx, r.JobID)
				version = j.Version
			}
			if _, e = s.retry(ctx, "a-rejected", j.ID, version, raws["a"]); e == nil {
				t.Fatal("conflicting retry accepted")
			}
			if competing != nil {
				if _, e = s.config.Engine.FailOrganize(ctx, competing.ID, competing.Attempt, "fixture failure"); e != nil {
					t.Fatal(e)
				}
			}
			if _, e = s.retry(ctx, "b-success", j.ID, j.Version, raws["b"]); e != nil {
				t.Fatal(e)
			}
			j, _ = s.config.Engine.GetJob(ctx, j.ID)
			session, e := s.jobConfiguration(ctx, j, "original")
			if e != nil || len(session.PublicationAuthorizations) != 1 || !reflect.DeepEqual(session.PublicationAuthorizations[0], raws["b"]) {
				t.Fatal(session, e)
			}
			revokeRetryKey(t, authority, keys, "a")
			release, e := s.publishGuard(session)(ctx)
			if e != nil {
				t.Fatal("rejected candidate poisoned success", e)
			}
			release()
		})
	}
}

func TestRetryAuthoritiesAccumulateAcrossRestartAndOriginalReplay(t *testing.T) {
	for _, revoked := range []string{"a", "b"} {
		t.Run(revoked, func(t *testing.T) {
			s, lib, authority, keys, raws := retryFixture(t)
			ctx := context.Background()
			source, e := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "source", LibraryID: lib.ID, Filename: "source.md", Bytes: []byte("原文")})
			if e != nil {
				t.Fatal(e)
			}
			j, input := acceptFailedJob(t, s, lib, "original", source.ResourceID, models.KnowledgeTaskConfiguration{})
			if _, e = s.retry(ctx, "a-retry", j.ID, j.Version, raws["a"]); e != nil {
				t.Fatal(e)
			}
			j, _ = s.config.Engine.GetJob(ctx, j.ID)
			if _, e = s.config.Engine.FailOrganize(ctx, j.ID, j.Attempt, "again"); e != nil {
				t.Fatal(e)
			}
			j, _ = s.config.Engine.GetJob(ctx, j.ID)
			if _, e = s.retry(ctx, "b-retry", j.ID, j.Version, raws["b"]); e != nil {
				t.Fatal(e)
			}
			j, _ = s.config.Engine.GetJob(ctx, j.ID)
			session, e := s.jobConfiguration(ctx, j, "original")
			if e != nil || len(session.PublicationAuthorizations) != 2 {
				t.Fatal(session, e)
			}
			restarted := New(s.config)
			session, e = restarted.jobConfiguration(ctx, j, "original")
			if e != nil {
				t.Fatal(e)
			}
			revokeRetryKey(t, authority, keys, revoked)
			if release, e := restarted.publishGuard(session)(ctx); e == nil {
				release()
				t.Fatal("revoked accumulated authority survived restart")
			}
			// 原接纳重放不得以最初内部配置启动当前尝试；真正提交前仍需原附加授权。
			var denied atomic.Int32
			restarted.config.IntegrationPublicationGuard = func(ctx context.Context, raws []json.RawMessage, runID string) (func(), error) {
				release, e := authority.RestoreKnowledgeGuards(ctx, raws, runID)
				if len(raws) == 2 && errors.Is(e, status_error.IntegrationUnauthorized) {
					denied.Add(1)
				}
				return release, e
			}
			restarted.config.Generator = func(context.Context, models.LibraryExecutionDefaults) (core.Generator, error) {
				return remoteIndexGenerator(func(context.Context, core.GenerateRequest) (core.GenerateResult, error) {
					return core.GenerateResult{}, nil
				}), nil
			}
			restarted.ctx, restarted.cancel = context.WithCancel(ctx)
			t.Cleanup(restarted.Close)
			if _, _, e = restarted.Organize(ctx, input); e != nil {
				t.Fatal(e)
			}
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				current, _ := s.config.Engine.GetJob(ctx, j.ID)
				if current.State == "failed" {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			current, _ := s.config.Engine.GetJob(ctx, j.ID)
			if current.State != "failed" || denied.Load() == 0 {
				t.Fatalf("current=%+v denied=%d", current, denied.Load())
			}
			chunks, e := s.config.Engine.ListDocumentChunks(ctx, lib.ID, source.ResourceID)
			if e != nil || len(chunks) > 0 {
				t.Fatal("revoked replay committed index", chunks, e)
			}
			if _, e = restarted.Retry(ctx, "internal-bypass", j.ID, current.Version); e == nil {
				t.Fatal("internal retry bypassed revoked original key")
			}
		})
	}
}

func TestOriginalExternalAuthorityCannotBeReplacedOnRetry(t *testing.T) {
	s, lib, authority, keys, raws := retryFixture(t)
	ctx := context.Background()
	source, e := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "source", LibraryID: lib.ID, Filename: "source.md", Bytes: []byte("原文")})
	if e != nil {
		t.Fatal(e)
	}
	j, _ := acceptFailedJob(t, s, lib, "external", source.ResourceID, models.KnowledgeTaskConfiguration{RemoteSubject: "a", RemoteAuthorization: raws["a"]})
	revokeRetryKey(t, authority, keys, "a")
	if _, e = s.retry(ctx, "b-bypass", j.ID, j.Version, raws["b"]); !errors.Is(e, status_error.IntegrationUnauthorized) {
		t.Fatal(e)
	}
}

func TestOldGeneratorPreparationCannotDriveRetriedAttempt(t *testing.T) {
	s, lib, authority, keys, raws := retryFixture(t)
	ctx := context.Background()
	strategy := core.IndexingStrategy{KeywordEnabled: true}
	if _, e := s.config.Engine.UpdateLibrary(ctx, core.UpdateLibraryInput{RequestID: "index", LibraryID: lib.ID, ExpectedVersion: lib.Version, Name: lib.Name, PolicyVersion: lib.PolicyVersion, IndexingStrategy: &strategy}); e != nil {
		t.Fatal(e)
	}
	lib, _ = s.config.Engine.GetLibrary(ctx, lib.ID)
	source, e := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "source", LibraryID: lib.ID, Filename: "source.md", Bytes: []byte("# 内容\n旧驱动不得把撤权后新尝试写入索引。")})
	if e != nil {
		t.Fatal(e)
	}
	entered := make(chan struct{}, 1)
	unblock := make(chan struct{})
	var calls atomic.Int32
	s.config.Generator = func(ctx context.Context, _ models.LibraryExecutionDefaults) (core.Generator, error) {
		if calls.Add(1) == 1 {
			entered <- struct{}{}
			select {
			case <-unblock:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return remoteIndexGenerator(func(context.Context, core.GenerateRequest) (core.GenerateResult, error) {
			return core.GenerateResult{}, nil
		}), nil
	}
	accepted, _, e := s.Organize(ctx, OrganizeCommand{RequestID: "original", LibraryID: lib.ID, SourceVersionIDs: []string{source.ResourceID}, ExplicitSources: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Start(ctx); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("generator preparation not reached")
	}
	job, _ := s.config.Engine.GetJob(ctx, accepted.JobID)
	oldAttempt := job.Attempt
	if _, e = s.config.Engine.FailOrganize(ctx, job.ID, job.Attempt, "failure during preparation"); e != nil {
		t.Fatal(e)
	}
	job, _ = s.config.Engine.GetJob(ctx, job.ID)
	if _, e = s.retry(ctx, "retry", job.ID, job.Version, raws["a"]); e != nil {
		t.Fatal(e)
	}
	origin, _ := s.config.References.GetTaskConfiguration(ctx, "original")
	if release, e := s.publishAttemptGuard(origin, job.ID, oldAttempt)(ctx); !errors.Is(e, status_error.KnowledgeCancelled) {
		if e == nil {
			release()
		}
		t.Fatal("old attempt guard", e)
	}
	revokeRetryKey(t, authority, keys, "a")
	close(unblock)
	s.workers.Wait()
	chunks, e := s.config.Engine.ListDocumentChunks(ctx, lib.ID, source.ResourceID)
	if e != nil || len(chunks) != 0 {
		t.Fatal("old preparation committed index", chunks, e)
	}
	sourceNow, e := s.config.Engine.GetSource(ctx, lib.ID, source.ResourceID)
	if e != nil || sourceNow.State == "processed" {
		t.Fatal("old preparation committed source conclusion", sourceNow, e)
	}
}

func TestRetryReplayRecoversAcceptedAttemptAfterRequestCancellation(t *testing.T) {
	for _, path := range []string{"call", "receipt"} {
		for _, revoked := range []bool{false, true} {
			name := path + "/authorized"
			if revoked {
				name = path + "/revoked-original"
			}
			t.Run(name, func(t *testing.T) {
				s, lib, authority, keys, raws := retryFixture(t)
				ctx := context.Background()
				source, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "source", LibraryID: lib.ID, Filename: "source.md", Bytes: []byte("持久来源")})
				if err != nil {
					t.Fatal(err)
				}
				job, _ := acceptFailedJob(t, s, lib, "original", source.ResourceID, models.KnowledgeTaskConfiguration{RemoteSubject: "a", RemoteAuthorization: raws["a"]})
				identity, err := authority.Authenticate(ctx, "b")
				if err != nil {
					t.Fatal(err)
				}
				bindings, err := s.PublishedBindings(ctx, authority, identity)
				if err != nil {
					t.Fatal(err)
				}
				var retry *agentcapability.Binding
				for i := range bindings {
					if bindings[i].Operation == "knowledge.jobs.retry" {
						retry = &bindings[i]
						break
					}
				}
				if retry == nil {
					t.Fatal("published retry binding missing")
				}
				s.config.Events = events.New()
				subscription := s.config.Events.Subscribe("knowledge", events.Resource{})
				defer subscription.Close()
				requestCtx, cancelRequest := context.WithCancel(ctx)
				defer cancelRequest()
				var cancelled atomic.Bool
				s.config.IntegrationPublicationGuard = func(ctx context.Context, raws []json.RawMessage, runID string) (func(), error) {
					release, e := authority.RestoreKnowledgeGuards(ctx, raws, runID)
					if e != nil {
						return nil, e
					}
					return func() {
						release()
						if cancelled.CompareAndSwap(false, true) {
							cancelRequest()
						}
					}, nil
				}
				request := agentcapability.Request{Operation: "knowledge.jobs.retry", RequestID: "retry", Params: map[string]any{"libraryId": lib.ID, "jobId": job.ID, "expectedVersion": job.Version}}
				if _, err = retry.Call(requestCtx, request); err == nil || requestCtx.Err() == nil {
					t.Fatalf("request cancellation not injected: %v", err)
				}
				actual, err := s.config.Engine.GetJob(ctx, job.ID)
				if err != nil || actual.Attempt != 2 || actual.State != "queued" {
					t.Fatalf("retry was not durably accepted: %+v %v", actual, err)
				}
				select {
				case event := <-subscription.Events:
					if event.Type != "knowledge.changed" {
						t.Fatal(event.Type)
					}
				default:
					t.Fatal("retry acceptance event missing")
				}
				select {
				case <-subscription.Events:
					t.Fatal("retry emitted duplicate acceptance events")
				default:
				}
				session, err := s.jobConfiguration(ctx, actual, "original")
				if err != nil || len(session.PublicationAuthorizations) != 1 {
					t.Fatal(session, err)
				}
				s.config.IntegrationPublicationGuard = authority.RestoreKnowledgeGuards
				entered := make(chan struct{}, 1)
				s.config.Generator = func(ctx context.Context, _ models.LibraryExecutionDefaults) (core.Generator, error) {
					entered <- struct{}{}
					<-ctx.Done()
					return nil, ctx.Err()
				}
				s.ctx, s.cancel = context.WithCancel(ctx)
				t.Cleanup(s.Close)
				if revoked {
					revokeRetryKey(t, authority, keys, "a")
				}
				var result agentcapability.Result
				if path == "call" {
					result, err = retry.Call(ctx, request)
				} else {
					result, err = retry.Receipt(ctx, request.RequestID)
				}
				if revoked {
					if !errors.Is(err, status_error.IntegrationUnauthorized) {
						t.Fatalf("revoked original authority replay: result=%+v err=%v", result, err)
					}
					select {
					case <-entered:
						t.Fatal("revoked replay drove generator")
					default:
					}
				} else {
					if err != nil || !result.Replayed {
						t.Fatalf("replay failed: %+v %v", result, err)
					}
					select {
					case <-entered:
					case <-time.After(time.Second):
						t.Fatal("accepted retry replay did not resume queued attempt")
					}
				}
				select {
				case <-subscription.Events:
					t.Fatal("receipt replay repeated acceptance event")
				default:
				}
				actual, err = s.config.Engine.GetJob(ctx, job.ID)
				if err != nil || actual.Attempt != 2 {
					t.Fatalf("replay retried core job again: %+v %v", actual, err)
				}
				session, err = s.jobConfiguration(ctx, actual, "original")
				if err != nil || len(session.PublicationAuthorizations) != 1 {
					t.Fatalf("replay appended authorization: %+v %v", session, err)
				}
			})
		}
	}
}
