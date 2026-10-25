package knowledge_connections

import (
	"context"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/remote"
)

// remoteClients 仅包含远端HTTP适配器，不能访问本机知识引擎或Runtime。
type remoteClients struct {
	// options 保存本次同一快照中的地址与凭据，仅在请求内使用，不输出 JSON。
	options remote.Options
	// libraryID 是本次固定的目标库；空值只用于目录管理。
	libraryID string
	// connection 为固定连接配置。
	connection models.KnowledgeConnection
	// official 为官方协议分支。
	official *remote.WeKnora
	// custom 为自有协议分支。
	custom *remote.Custom
}

func client(ctx context.Context, connectionID, libraryID string) (*remoteClients, error) {
	if global.KnowledgeConnectionControllerManager == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	connection, token, err := global.KnowledgeConnectionControllerManager.Snapshot(ctx, connectionID)
	if err != nil {
		return nil, err
	}
	if !connection.Enabled {
		return nil, status_error.KnowledgeForbidden
	}
	if connection.Backend == "integration" && libraryID != "" {
		if global.KnowledgeRemoteMountControllerManager == nil {
			return nil, status_error.KnowledgeUnsupported
		}
		mounts, e := global.KnowledgeRemoteMountControllerManager.List(ctx, connectionID)
		if e != nil {
			return nil, e
		}
		found := false
		for _, mount := range mounts {
			if mount.KnowledgeBaseID == libraryID {
				found = true
				break
			}
		}
		if !found {
			return nil, status_error.KnowledgeNotFound
		}
	}
	return clientSnapshot(connection, token, libraryID)
}
func clientSnapshot(connection models.KnowledgeConnection, token, libraryID string) (*remoteClients, error) {
	connectionID := connection.ID
	var err error
	out := &remoteClients{connection: connection, libraryID: libraryID}
	options := remote.Options{BaseURL: connection.BaseURL, Token: token, AuthMode: connection.AuthMode}
	out.options = options
	switch connection.Backend {
	case "integration":
		out.official, err = remote.NewWeKnora(options)
		if err == nil && connection.Detection.ServiceKind == "neuxally" {
			out.custom, err = remote.NewIntegration(options, remote.ResourceRef{Backend: "integration", ConnectionID: connectionID, KnowledgeBaseID: libraryID})
		}
	case "weknora":
		out.official, err = remote.NewWeKnora(options)
	case "custom":
		out.custom, err = remote.NewCustom(options, remote.ResourceRef{Backend: "custom", ConnectionID: connectionID, KnowledgeBaseID: libraryID})
	default:
		return nil, status_error.KnowledgeUnsupported
	}
	return out, remoteError(err)
}
func remoteError(err error) error {
	if err == nil {
		return nil
	}
	var e *remote.Error
	if errors.As(err, &e) {
		switch e.Code {
		case "unauthenticated":
			return status_error.KnowledgeRemoteUnauthorized
		case "forbidden", "scope_violation", "policy_changed", "write_ip_forbidden":
			return status_error.KnowledgeForbidden
		case "not_found":
			return status_error.KnowledgeNotFound
		case "version_conflict", "idempotency_conflict", "manual_protected":
			return status_error.KnowledgeConflict
		case "unsupported", "unsupported_profile", "unsupported_source_state":
			return status_error.KnowledgeUnsupported
		case "invalid", "invalid_connection", "invalid_resource", "invalid_request", "invalid_query", "invalid_pagination":
			return status_error.KnowledgeInvalid
		case "cancelled", "stale_attempt":
			return status_error.KnowledgeCancelled
		case "candidate_invalid", "execution_not_successful":
			return status_error.KnowledgeCandidateInvalid
		case "protocol_incompatible", "redirect_rejected", "response_too_large", "response_incomplete":
			return status_error.KnowledgeRemoteIncompatible
		case "outcome_unknown":
			return status_error.KnowledgeRemoteOutcomeUnknown
		}
	}
	return status_error.KnowledgeRemoteUnreachable
}
