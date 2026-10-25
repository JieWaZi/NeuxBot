package knowledge_connections

import (
	"github.com/JieWaZi/neux-knowledge/remote"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取远端知识能力
type GetRemoteCapabilities struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
}

func (o *GetRemoteCapabilities) Path() string {
	return "/:connectionId/libraries/:libraryId/capabilities"
}
func (o *GetRemoteCapabilities) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	out := RemoteCapabilities{Ref: remoteRef(c, o.LibraryID), Operations: map[string]bool{}, Permissions: map[string]string{}, Reasons: map[string]string{}, Details: map[string]RemoteCapabilityStatus{}, Profiles: []RemoteProfile{}}
	out.ServiceKind = c.connection.Detection.ServiceKind
	out.ServiceVersion = c.connection.Detection.Version
	out.UploadMode = c.connection.Detection.UploadMode
	out.ConnectionRevision = c.connection.Revision
	out.Evidence = c.connection.Detection.Evidence
	if c.connection.Backend == "integration" && c.custom == nil {
		out.Profile = c.connection.Detection.Version
		for name, value := range c.connection.Detection.Operations {
			out.Details[name] = RemoteCapabilityStatus{Supported: value.Supported, Permission: value.Permission, Condition: value.Condition}
			out.Permissions[name] = value.Permission
			out.Reasons[name] = value.Condition
			out.Operations[name] = value.Supported && value.Permission == "allowed" && (value.Condition == "ready")
		}
		return out, nil
	}
	if c.custom != nil {
		v, e := c.custom.Capabilities(ctx)
		if e != nil {
			return nil, remoteError(e)
		}
		out.Profile = v.Profile
		out.Operations = v.Operations
		for key := range v.Operations {
			// 缺少明细时保留未知权限，不能用可用操作反推授权。
			out.Permissions[key] = "unknown"
			out.Reasons[key] = "capability_details_unavailable"
		}
		for key, value := range v.Details {
			out.Details[key] = RemoteCapabilityStatus{Supported: value.Supported, Permission: value.Permission, Condition: value.Condition}
			out.Permissions[key] = value.Permission
			switch {
			case value.Condition != "ready":
				out.Reasons[key] = value.Condition
			case !value.Supported:
				out.Reasons[key] = "remote_capability_unavailable"
			case value.Permission == "denied":
				out.Reasons[key] = "permission_denied"
			default:
				out.Reasons[key] = ""
			}
		}
		for _, p := range v.Profiles {
			out.Profiles = append(out.Profiles, RemoteProfile{ID: p.ID, Name: p.Name})
		}
		return out, nil
	}
	out.Profile = remote.WeKnoraProfile
	caps := c.official.Capabilities()
	for key, original := range map[string]string{"read": "pages", "write": "versioned_edit", "rawOnly": "raw_upload", "organize": "organize", "rebuild": "organize", "maintain": "versioned_edit", "history": "revisions", "atomicRestore": "atomic_restore", "sourceArchive": "archive", "sourceRead": "sources", "sourceDownload": "source_download", "sourceContent": "versioned_edit", "sourcePurge": "archive", "libraryDelete": "libraryDelete", "activity": "versioned_edit", "graph": "graph", "themes": "folders", "themeWrite": "versioned_edit", "softwareRules": "software_rules"} {
		v := caps[original]
		out.Operations[key] = v.Supported
		out.Permissions[key] = v.Permission
		out.Reasons[key] = v.Reason
		condition := "ready"
		if !v.Supported {
			condition = v.Reason
		}
		out.Details[key] = RemoteCapabilityStatus{Supported: v.Supported, Permission: v.Permission, Condition: condition}
	}
	return out, nil
}
