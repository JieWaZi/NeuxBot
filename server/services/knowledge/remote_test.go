package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/httpapi"
	"strings"
	"testing"
)

// remoteIndexGenerator 让回归只检查索引提交，不依赖真实模型正文。
type remoteIndexGenerator func(context.Context, core.GenerateRequest) (core.GenerateResult, error)

func (generate remoteIndexGenerator) Generate(ctx context.Context, in core.GenerateRequest) (core.GenerateResult, error) {
	return generate(ctx, in)
}

func TestRemoteRevocationDuringEmbeddingCannotPersistChunks(t *testing.T) {
	s, library := newServiceFixture(t)
	ctx := context.Background()
	model := "index-model"
	strategy := core.IndexingStrategy{VectorEnabled: true, KeywordEnabled: true}
	if _, err := s.config.Engine.UpdateLibrary(ctx, core.UpdateLibraryInput{RequestID: "rag-config", LibraryID: library.ID, ExpectedVersion: library.Version, Name: library.Name, PolicyVersion: library.PolicyVersion, EmbeddingModelID: &model, IndexingStrategy: &strategy}); err != nil {
		t.Fatal(err)
	}
	library, _ = s.config.Engine.GetLibrary(ctx, library.ID)
	source, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "rag-source", LibraryID: library.ID, Filename: "source.md", Bytes: []byte(strings.Repeat("撤权后迟到的向量不得进入已接纳来源的检索索引。", 20))})
	if err != nil {
		t.Fatal(err)
	}
	token := "remote-index-token"
	permission := core.PermissionSnapshot{PolicyVersion: library.PolicyVersion, Operations: []string{"read", "organize"}}
	security, err := httpapi.NewPolicyStore(httpapi.SecurityConfig{Tokens: map[string]httpapi.Principal{token: {Subject: "index-user", Libraries: map[string]httpapi.LibraryGrant{library.ID: {Operations: permission.Operations, PolicyVersion: library.PolicyVersion}}}}})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewRemoteAdapter(s, ProfileMap{"server-default": {RuntimeID: constants.RuntimeIDCodex}}, security)
	if err != nil {
		t.Fatal(err)
	}
	embedded := false
	s.config.FreezeEmbedder = func(context.Context, string, string) (core.DocumentEmbedder, error) {
		return core.DocumentEmbedder{Fingerprint: "index-space", Embed: func(_ context.Context, texts []string) ([][]float32, error) {
			// 真实撤权须在外部嵌入调用期间完成，不能被索引持有的发布锁阻塞。
			if err := security.Replace(httpapi.SecurityConfig{}); err != nil {
				return nil, err
			}
			embedded = true
			vectors := make([][]float32, len(texts))
			for index := range texts {
				vectors[index] = []float32{1, 0}
			}
			return vectors, nil
		}}, nil
	}
	s.config.Generator = func(context.Context, models.LibraryExecutionDefaults) (core.Generator, error) {
		return remoteIndexGenerator(func(context.Context, core.GenerateRequest) (core.GenerateResult, error) {
			return core.GenerateResult{}, status_error.RuntimeUnavailable
		}), nil
	}
	digest := sha256.Sum256([]byte(token))
	identity := httpapi.RecoveryIdentity{Subject: "index-user", CredentialSHA256: hex.EncodeToString(digest[:]), LibraryID: library.ID, Operation: "organize", RemoteAddr: "127.0.0.1:1234", Permission: permission}
	auth := httpapi.Authorization{Subject: identity.Subject, Permission: permission, Recovery: identity, PublishGuard: security.RestoreGuard(s.config.Engine, identity)}
	receipt, err := adapter.Organize(ctx, httpapi.OrganizeRequest{RequestID: "remote-rag", LibraryID: library.ID, ExpectedVersion: library.Version, StructureVersion: library.StructureVersion, SourceVersionIDs: []string{source.ResourceID}, ProfileID: "server-default"}, auth)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.workers.Wait()
	if !embedded {
		t.Fatal("test did not reach the external embedding response")
	}
	chunks, err := s.config.Engine.ListDocumentChunks(ctx, library.ID, source.ResourceID)
	if err != nil || len(chunks) != 0 {
		t.Fatalf("revoked remote job %s persisted chunks: %+v error=%v", receipt.JobID, chunks, err)
	}
}

func TestRemoteAdapterPersistsRecoverableIdentityAndRejectsRevokedCredential(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	raw, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "raw", LibraryID: lib.ID, Filename: "source.md", Bytes: []byte("Evidence")})
	if err != nil {
		t.Fatal(err)
	}
	token := "remote-secret"
	sum := sha256.Sum256([]byte(token))
	permission := core.PermissionSnapshot{PolicyVersion: lib.PolicyVersion, Operations: []string{"read", "organize"}, Scope: []string{}}
	security, err := httpapi.NewPolicyStore(httpapi.SecurityConfig{Tokens: map[string]httpapi.Principal{token: {Subject: "server-user", Libraries: map[string]httpapi.LibraryGrant{lib.ID: {Operations: permission.Operations, PolicyVersion: lib.PolicyVersion}}}}})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewRemoteAdapter(s, ProfileMap{"server-default": {RuntimeID: constants.RuntimeIDCodex}}, security)
	if err != nil {
		t.Fatal(err)
	}
	identity := httpapi.RecoveryIdentity{Subject: "server-user", CredentialSHA256: hex.EncodeToString(sum[:]), LibraryID: lib.ID, Operation: "organize", RemoteAddr: "127.0.0.1:1234", Permission: permission}
	auth := httpapi.Authorization{Subject: identity.Subject, Permission: permission, Recovery: identity, PublishGuard: security.RestoreGuard(s.config.Engine, identity)}
	receipt, err := adapter.Organize(ctx, httpapi.OrganizeRequest{RequestID: "remote", LibraryID: lib.ID, ExpectedVersion: lib.Version, StructureVersion: lib.StructureVersion, SourceVersionIDs: []string{raw.ResourceID}, ProfileID: "server-default"}, auth)
	if err != nil {
		t.Fatal(err)
	}
	job, _ := s.config.Engine.GetJob(ctx, receipt.ResourceID)
	if len(job.SourceVersionIDs) != 1 || job.SourceVersionIDs[0] != raw.ResourceID {
		t.Fatalf("remote job expanded sources: %+v", job)
	}
	session, err := s.config.References.GetTaskConfiguration(ctx, "remote")
	if err != nil || session.RemoteSubject != "server-user" || session.RemoteProfileID != "server-default" {
		t.Fatalf("session=%+v err=%v", session, err)
	}
	var persisted httpapi.RecoveryIdentity
	if err := json.Unmarshal(session.RemoteAuthorization, &persisted); err != nil || persisted.CredentialSHA256 != identity.CredentialSHA256 {
		t.Fatalf("recovery=%+v err=%v", persisted, err)
	}
	if err := security.Replace(httpapi.SecurityConfig{}); err != nil {
		t.Fatal(err)
	}
	if release, err := s.publishGuard(session)(ctx); err == nil {
		release()
		t.Fatal("revoked original credential was allowed to publish")
	}
}

func TestRemoteAdapterRebuildResetsOnlyExplicitSource(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	chosen, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "chosen", LibraryID: lib.ID, Filename: "chosen.md", Bytes: []byte("chosen")})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "other", LibraryID: lib.ID, Filename: "other.md", Bytes: []byte("other")})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.config.Engine.GetSource(ctx, lib.ID, chosen.ResourceID)
	untouched, _ := s.config.Engine.GetSource(ctx, lib.ID, other.ResourceID)
	token := "remote-secret"
	sum := sha256.Sum256([]byte(token))
	permission := core.PermissionSnapshot{PolicyVersion: lib.PolicyVersion, Operations: []string{"read", "organize"}}
	security, err := httpapi.NewPolicyStore(httpapi.SecurityConfig{Tokens: map[string]httpapi.Principal{token: {Subject: "server-user", Libraries: map[string]httpapi.LibraryGrant{lib.ID: {Operations: permission.Operations, PolicyVersion: lib.PolicyVersion}}}}})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewRemoteAdapter(s, ProfileMap{"server-default": {RuntimeID: constants.RuntimeIDCodex}}, security)
	if err != nil {
		t.Fatal(err)
	}
	identity := httpapi.RecoveryIdentity{Subject: "server-user", CredentialSHA256: hex.EncodeToString(sum[:]), LibraryID: lib.ID, Operation: "organize", RemoteAddr: "127.0.0.1:1234", Permission: permission}
	auth := httpapi.Authorization{Subject: identity.Subject, Permission: permission, Recovery: identity, PublishGuard: security.RestoreGuard(s.config.Engine, identity)}
	raw, _ := json.Marshal(map[string]any{"requestId": "remote-rebuild", "libraryId": lib.ID, "expectedVersion": lib.Version, "structureVersion": lib.StructureVersion, "sourceVersionIds": []string{chosen.ResourceID}, "profileId": "server-default", "rebuild": true, "instruction": "保留边界与来源"})
	var in httpapi.OrganizeRequest
	_ = json.Unmarshal(raw, &in)
	receipt, err := adapter.Organize(ctx, in, auth)
	if err != nil || receipt.JobID == "" {
		t.Fatal(receipt, err)
	}
	after, _ := s.config.Engine.GetSource(ctx, lib.ID, chosen.ResourceID)
	otherAfter, _ := s.config.Engine.GetSource(ctx, lib.ID, other.ResourceID)
	if after.RowVersion != before.RowVersion+1 || otherAfter.RowVersion != untouched.RowVersion {
		t.Fatalf("remote rebuild lost selected-source semantics: chosen=%+v, other=%+v", after, otherAfter)
	}
	session, err := s.config.References.GetTaskConfiguration(ctx, "remote-rebuild")
	if err != nil || session.Message != "保留边界与来源" {
		t.Fatalf("remote instruction was discarded: %q %v", session.Message, err)
	}
	replay, err := adapter.Organize(ctx, in, auth)
	if err != nil || replay.JobID != receipt.JobID {
		t.Fatal("rebuild replay changed job", replay, err)
	}
	in.Instruction = "改变原要求"
	var rejected *httpapi.APIError
	if _, err := adapter.Organize(ctx, in, auth); !errors.As(err, &rejected) || rejected.Status != 409 {
		t.Fatal("same request changed its instruction", err)
	}
}
