package knowledge

import (
	"context"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	keyrules "github.com/JieWaZi/NeuxBot/server/internal/integration"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	integrationservice "github.com/JieWaZi/NeuxBot/server/services/integration"
	"github.com/JieWaZi/neux-knowledge/core"
	"strings"
	"testing"
)

func TestPublishedLateEmbeddingAndRestartKeepOriginalAuthority(t *testing.T) {
	s, lib, _, db := newServiceFixtureWithDB(t)
	ctx := context.Background()
	store := controllers.NewIntegrationKeyController(db)
	_, _, err := store.ClaimMutation(ctx, models.IntegrationKeyMutation{RequestID: "create-key", Digest: "fixture", KeyID: "key", Action: "create"})
	if err != nil {
		t.Fatal(err)
	}
	key, err := store.CompleteMutation(ctx, "create-key", "fixture", models.IntegrationKey{ID: "key", Name: "集成", KeyHash: keyrules.HashKey("fixture"), Enabled: true, Grants: []models.IntegrationGrant{{ResourceKind: constants.IntegrationResourceKindKnowledge, ResourceIDs: []string{lib.ID}, Capabilities: []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve, constants.IntegrationCapabilityOrganize}}}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	authority := integrationservice.New(integrationservice.Config{Keys: store})
	s.config.IntegrationGuard = authority.RestoreKnowledgeGuard
	model := "index-model"
	strategy := core.IndexingStrategy{VectorEnabled: true, KeywordEnabled: true}
	_, err = s.config.Engine.UpdateLibrary(ctx, core.UpdateLibraryInput{RequestID: "index-config", LibraryID: lib.ID, ExpectedVersion: lib.Version, Name: lib.Name, PolicyVersion: lib.PolicyVersion, EmbeddingModelID: &model, IndexingStrategy: &strategy})
	if err != nil {
		t.Fatal(err)
	}
	lib, _ = s.config.Engine.GetLibrary(ctx, lib.ID)
	source, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "source", LibraryID: lib.ID, Filename: "source.md", Bytes: []byte(strings.Repeat("原授权撤销后，迟到的向量和资料状态不可继续提交。", 20))})
	if err != nil {
		t.Fatal(err)
	}
	identity, _ := authority.Authenticate(ctx, "fixture")
	auth, err := authority.Authorize(ctx, identity, constants.IntegrationResourceKindKnowledge, lib.ID, []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve, constants.IntegrationCapabilityOrganize})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(integrationservice.KnowledgeRecovery{Origin: "direct_api", Subject: "key", Authorization: auth, Permission: core.PermissionSnapshot{PolicyVersion: lib.PolicyVersion, Scope: []string{}, Operations: []string{"organize"}}})
	embedded := false
	s.config.FreezeEmbedder = func(context.Context, string, string) (core.DocumentEmbedder, error) {
		return core.DocumentEmbedder{Fingerprint: "fixture-index", Embed: func(_ context.Context, texts []string) ([][]float32, error) {
			err := authority.WithAuthorizationWrite(ctx, func(ctx context.Context) error {
				key.Enabled = false
				_, err := store.UpdateAuthorization(ctx, key, key.Revision)
				return err
			})
			if err != nil {
				return nil, err
			}
			embedded = true
			vectors := make([][]float32, len(texts))
			for i := range vectors {
				vectors[i] = []float32{1, 0}
			}
			return vectors, nil
		}}, nil
	}
	s.config.Generator = func(context.Context, models.LibraryExecutionDefaults) (core.Generator, error) {
		return remoteIndexGenerator(func(context.Context, core.GenerateRequest) (core.GenerateResult, error) {
			return core.GenerateResult{}, status_error.RuntimeUnavailable
		}), nil
	}
	receipt, session, err := s.Organize(ctx, OrganizeCommand{RequestID: "published", LibraryID: lib.ID, SourceVersionIDs: []string{source.ResourceID}, ExplicitSources: true, Configuration: models.KnowledgeTaskConfiguration{RemoteSubject: "key", RemoteAuthorization: raw}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.workers.Wait()
	if !embedded {
		t.Fatal("did not exercise late external embedding response")
	}
	chunks, err := s.config.Engine.ListDocumentChunks(ctx, lib.ID, source.ResourceID)
	if err != nil || len(chunks) != 0 {
		t.Fatal("late chunks committed", receipt, chunks, err)
	}
	actual, err := s.config.Engine.GetSource(ctx, lib.ID, source.ResourceID)
	if err != nil || actual.State == "processed" {
		t.Fatal("late source conclusion committed", actual, err)
	}
	restarted := integrationservice.New(integrationservice.Config{Keys: store})
	s.config.IntegrationGuard = restarted.RestoreKnowledgeGuard
	if release, err := s.publishGuard(session)(ctx); err == nil {
		release()
		t.Fatal("restart changed original revoked identity")
	}
}
