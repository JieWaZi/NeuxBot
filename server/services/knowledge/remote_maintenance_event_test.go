package knowledge

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/httpapi"
)

func TestRemoteMaintenanceEmitsOnlyCommittedChanges(t *testing.T) {
	for _, outcome := range []string{"success", "generation-failed", "write-conflict"} {
		t.Run(outcome, func(t *testing.T) {
			s, lib := newServiceFixture(t)
			ctx := context.Background()
			created, err := s.config.Engine.WritePage(ctx, core.WritePageInput{RequestID: "page", LibraryID: lib.ID, StructureVersion: lib.StructureVersion, Page: core.Page{Slug: "concept/topic", Title: "主题", PageType: "concept", Content: "旧内容"}})
			if err != nil {
				t.Fatal(err)
			}
			lib, _ = s.config.Engine.GetLibrary(ctx, lib.ID)
			page, _ := s.config.Engine.GetPage(ctx, lib.ID, created.ResourceID)
			s.config.Events = events.New()
			subscription := s.config.Events.Subscribe("knowledge", events.Resource{})
			defer subscription.Close()
			s.config.Generator = func(context.Context, models.LibraryExecutionDefaults) (core.Generator, error) {
				return remoteIndexGenerator(func(context.Context, core.GenerateRequest) (core.GenerateResult, error) {
					if outcome == "generation-failed" {
						return core.GenerateResult{}, status_error.RuntimeUnavailable
					}
					if outcome == "write-conflict" {
						next := *page
						next.Content = "其他已提交内容"
						if _, err := s.config.Engine.WritePage(ctx, core.WritePageInput{RequestID: "other-write", LibraryID: lib.ID, ExpectedVersion: page.Version, StructureVersion: lib.StructureVersion, Page: next}); err != nil {
							t.Fatal(err)
						}
					}
					return core.GenerateResult{Text: "维护后的内容", FinishReason: "stop"}, nil
				}), nil
			}
			adapter := RemoteAdapter{Service: s, Profiles: ProfileMap{"default": {RuntimeID: "codex"}}}
			receipt, err := adapter.Maintain(ctx, httpapi.MaintenanceRequest{RequestID: "maintain", ProfileID: "default", LibraryID: lib.ID, PageID: page.ID, ExpectedVersion: lib.Version, StructureVersion: lib.StructureVersion, ExpectedPageVersion: page.Version, Instruction: "修订"}, httpapi.Authorization{Subject: "user", Recovery: httpapi.RecoveryIdentity{Subject: "user"}, PublishGuard: func(context.Context) (func(), error) { return func() {}, nil }})
			if outcome != "success" {
				if err == nil {
					t.Fatal("failed maintenance reported success")
				}
				select {
				case event := <-subscription.Events:
					t.Fatal("failed maintenance emitted success event", event)
				default:
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case event := <-subscription.Events:
				current, e := s.config.Engine.GetPage(ctx, lib.ID, page.ID)
				if event.Type != "knowledge.content.changed" || event.Resource.ID != lib.ID || e != nil || current.Content != "维护后的内容" {
					t.Fatal("event did not identify committed page", event, current, e)
				}
				if receipt.ResourceID != page.ID {
					t.Fatal("wrong receipt", receipt)
				}
			default:
				t.Fatal("committed remote maintenance did not notify resource readers")
			}
		})
	}
}
