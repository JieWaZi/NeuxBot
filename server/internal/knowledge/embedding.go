package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/embedding"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
	"math"
)

// LibraryEmbeddings 仅组合此库配置、加密凭据与既有模型守卫，不访问记忆模块。
type LibraryEmbeddings struct {
	// Engine 是核心配置及模型更换守卫权威。
	Engine *core.Engine
	// References 保存本机独立嵌入配置和恢复回执。
	References controllers.KnowledgeController
}

// SaveLibraryEmbeddingInput 表达独立配置保存，两种版本均由用户已读值提供。
type SaveLibraryEmbeddingInput struct {
	// RequestID 是稳定请求身份。
	RequestID string
	// LibraryID 是唯一所属库。
	LibraryID string
	// ExpectedVersion 是配置CAS版本。
	ExpectedVersion int64
	// ExpectedLibraryVersion 是核心库CAS版本。
	ExpectedLibraryVersion int64
	// BaseURL 是嵌入服务地址。
	BaseURL string
	// Model 是模型名称。
	Model string
	// APIKey 仅输入，空值保留本库密钥。
	APIKey string
}

func (p LibraryEmbeddings) library(ctx context.Context, id string) (*core.Library, error) {
	if p.Engine == nil || p.References == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	lib, err := p.Engine.GetLibrary(ctx, id)
	if err != nil {
		return nil, MapError(err)
	}
	if lib.Archived || lib.Kind != "general" {
		return nil, status_error.KnowledgeForbidden
	}
	return lib, nil
}

// Get 返回脱敏配置，尚未配置时明确返回 version0。
func (p LibraryEmbeddings) Get(ctx context.Context, id string) (models.LibraryEmbeddingProfile, error) {
	lib, err := p.library(ctx, id)
	if err != nil {
		return models.LibraryEmbeddingProfile{}, err
	}
	v, err := p.References.LibraryEmbeddingProfile(ctx, id)
	v.LibraryVersion = lib.Version
	return v, err
}

// Save 先确认真实核心模型守卫，再提交宿主配置；未知结果重放首次冻结的核心更新。
func (p LibraryEmbeddings) Save(ctx context.Context, in SaveLibraryEmbeddingInput) (models.LibraryEmbeddingProfile, error) {
	lib, err := p.library(ctx, in.LibraryID)
	if err != nil {
		return models.LibraryEmbeddingProfile{}, err
	}
	if in.ExpectedLibraryVersion < 1 {
		return models.LibraryEmbeddingProfile{}, status_error.KnowledgeInvalid
	}
	base, model, err := controllers.NormalizeLibraryEmbedding(in.BaseURL, in.Model)
	if err != nil {
		return models.LibraryEmbeddingProfile{}, err
	}
	requestID := (requestidempotency.Identity{ID: in.RequestID}).ResourceID("library-embedding-core", lib.ID)
	update := core.UpdateLibraryInput{RequestID: requestID, LibraryID: lib.ID, ExpectedVersion: in.ExpectedLibraryVersion, Name: lib.Name, Purpose: lib.Purpose, Archived: lib.Archived, PolicyVersion: lib.PolicyVersion, EmbeddingModelID: &model}
	payload, _ := json.Marshal(update)
	prepared, err := p.References.PrepareLibraryEmbedding(ctx, controllers.PrepareLibraryEmbeddingInput{RequestID: in.RequestID, LibraryID: lib.ID, ExpectedVersion: in.ExpectedVersion, ExpectedLibraryVersion: in.ExpectedLibraryVersion, BaseURL: base, Model: model, APIKey: in.APIKey, CoreInput: payload})
	if err != nil {
		return models.LibraryEmbeddingProfile{}, err
	}
	if prepared.Committed {
		return prepared.Snapshot, nil
	}
	if err = json.Unmarshal(prepared.CoreInput, &update); err != nil {
		return models.LibraryEmbeddingProfile{}, status_error.KnowledgeStorageError
	}
	receipt, err := p.Engine.UpdateLibrary(ctx, update)
	if err != nil {
		return models.LibraryEmbeddingProfile{}, MapError(err)
	}
	return p.References.CommitLibraryEmbedding(ctx, in.RequestID, receipt.Version)
}

// SaveCreated 为已创建库补齐首次配置，重试不会再创建核心库。
func (p LibraryEmbeddings) SaveCreated(ctx context.Context, id, requestID, baseURL, model, key string) (models.LibraryEmbeddingProfile, error) {
	lib, err := p.library(ctx, id)
	if err != nil {
		return models.LibraryEmbeddingProfile{}, err
	}
	prepared, err := p.References.PrepareLibraryEmbedding(ctx, controllers.PrepareLibraryEmbeddingInput{RequestID: requestID, LibraryID: id, BaseURL: baseURL, Model: model, APIKey: key})
	if err != nil {
		return models.LibraryEmbeddingProfile{}, err
	}
	if prepared.Committed {
		return prepared.Snapshot, nil
	}
	// 已完成创建的回执先恢复原结果；只有尚未激活配置才核对当前模型，避免覆盖后来编辑。
	if lib.EmbeddingModelID != model {
		return models.LibraryEmbeddingProfile{}, status_error.KnowledgeConflict
	}
	return p.References.CommitLibraryEmbedding(ctx, requestID, lib.Version)
}

// Freeze 使用同一库的原子配置快照，不从记忆或其他库继承端点、模型或密钥。
func (p LibraryEmbeddings) Freeze(ctx context.Context, id, expectedModel string) (core.DocumentEmbedder, error) {
	profile, key, err := p.References.LibraryEmbeddingSnapshot(ctx, id)
	if err != nil {
		return core.DocumentEmbedder{}, err
	}
	if expectedModel != "" && profile.Model != expectedModel {
		return core.DocumentEmbedder{}, status_error.KnowledgeConflict
	}
	return libraryDocumentEmbedder(profile.BaseURL, profile.Model, key), nil
}
func libraryDocumentEmbedder(baseURL, model, key string) core.DocumentEmbedder {
	sum := sha256.Sum256([]byte(baseURL + "\x00" + model))
	client := embedding.NewOpenAIClient(embedding.Config{BaseURL: baseURL, Model: model, APIKey: key}, embedding.ErrorSet{
		RequestFailed:   status_error.KnowledgeEmbeddingUnavailable,
		Unavailable:     status_error.KnowledgeEmbeddingUnavailable,
		ResponseInvalid: status_error.KnowledgeEmbeddingResponseInvalid,
	})
	return core.DocumentEmbedder{Fingerprint: hex.EncodeToString(sum[:]), Embed: client.EmbedBatch}
}

// Test 只探测草稿，不保存；可选库身份仅用于读取该库已有密钥。
func (p LibraryEmbeddings) Test(ctx context.Context, id, baseURL, model, key string) (int, error) {
	baseURL, model, err := controllers.NormalizeLibraryEmbedding(baseURL, model)
	if err != nil {
		return 0, err
	}
	if id != "" {
		if _, err = p.library(ctx, id); err != nil {
			return 0, err
		}
	}
	if key == "" {
		if id == "" {
			return 0, status_error.KnowledgeEmbeddingNotConfigured
		}
		_, key, err = p.References.LibraryEmbeddingSnapshot(ctx, id)
		if err != nil {
			return 0, err
		}
	}
	vectors, err := libraryDocumentEmbedder(baseURL, model, key).Embed(ctx, []string{"Knowledge embedding connection test"})
	if err != nil {
		return 0, err
	}
	if len(vectors) != 1 || len(vectors[0]) == 0 {
		return 0, status_error.KnowledgeEmbeddingResponseInvalid
	}
	norm := 0.0
	for _, v := range vectors[0] {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return 0, status_error.KnowledgeEmbeddingResponseInvalid
		}
		norm += float64(v) * float64(v)
	}
	if norm == 0 {
		return 0, status_error.KnowledgeEmbeddingResponseInvalid
	}
	return len(vectors[0]), nil
}
