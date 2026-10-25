package integration_knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
)

func scopedRequest(ctx *gin.Context, libraryID, rawID string) (string, string, error) {
	if rawID == "" {
		rawID = ctx.GetHeader("Idempotency-Key")
	}
	if rawID == "" {
		rawID = ctx.GetHeader("X-Request-ID")
	}
	if header := ctx.GetHeader("Idempotency-Key"); header != "" && header != rawID {
		return "", "", status_error.KnowledgeInvalid
	}
	identity, err := requestidempotency.New(rawID, struct{}{})
	if err != nil {
		return "", "", err
	}
	principal, ok := middleware.IntegrationPrincipal(ctx.Request.Context())
	if !ok {
		return "", "", status_error.IntegrationUnauthorized
	}
	return identity.ID, identity.ResourceID("integration-knowledge", principal.KeyID, libraryID), nil
}

// performWrite 复用现有操作登记，比较原输入后恢复原回执，避免重读页面破坏幂等。
func performWrite(ctx *gin.Context, activity context.Context, requestID, operation, libraryID string, input any, write func() (*core.Receipt, error)) (*core.Receipt, error) {
	d := dependencies(ctx)
	if d.options.References == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	raw, err := json.Marshal(struct {
		// Operation 固定实际操作。
		Operation string
		// LibraryID 固定目标库。
		LibraryID string
		// Input 仅在内存中计算请求摘要。
		Input any
	}{operation, libraryID, input})
	if err != nil {
		return nil, status_error.KnowledgeInvalid
	}
	hash := sha256.Sum256(raw)
	if err := d.options.References.BeginOperation(activity, models.KnowledgeOperationLink{RequestID: requestID, Operation: operation, Digest: hex.EncodeToString(hash[:]), Backend: "local", ConnectionID: "local", KnowledgeBaseID: libraryID}); err != nil {
		return nil, err
	}
	receipt, err := d.engine.GetReceipt(activity, requestID)
	if err != nil {
		var code *core.Error
		if !errors.As(err, &code) || code.Code != core.ErrNotFound {
			return nil, bridge.MapError(err)
		}
		receipt, err = write()
		if err != nil {
			return nil, bridge.MapError(err)
		}
		if d.options.Events != nil {
			_ = events.Emit(d.options.Events, "knowledge", "knowledge.changed", events.Resource{Kind: "knowledge_library", ID: libraryID, ParentID: "local"}, map[string]any{"ref": bridge.LocalRef(libraryID), "requestId": requestID, "resourceId": receipt.ResourceID})
		}
	}
	if err := d.options.References.CompleteOperation(activity, requestID, receipt.LibraryID, receipt.ResourceID, receipt.JobID); err != nil {
		return nil, err
	}
	return receipt, nil
}

func publicReceipt(receipt *core.Receipt, requestID string) *core.Receipt {
	if receipt == nil {
		return nil
	}
	copy := *receipt
	copy.RequestID = requestID
	return &copy
}
