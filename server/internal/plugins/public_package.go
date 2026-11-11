package plugins

import "net/url"

// PublicManifest 是目录与已安装插件共用的公开元数据。
type PublicManifest struct {
	// 规范标识
	Schema string `json:"$schema,omitempty"`
	// 包名称
	Name string `json:"name"`
	// 包版本
	Version string `json:"version,omitempty"`
	// 能力说明
	Description string `json:"description,omitempty"`
	// 作者资料
	Author Author `json:"author"`
	// 项目主页
	Homepage string `json:"homepage,omitempty"`
	// 源码仓库
	Repository string `json:"repository,omitempty"`
	// 许可标识
	License string `json:"license,omitempty"`
	// 包标签
	Keywords []string `json:"keywords"`
}

// PublicAuthentication 保留授权交互提示，排除程序入口与交付绑定。
type PublicAuthentication struct {
	// 授权策略
	Strategy string `json:"strategy"`
	// 授权协议
	Protocol string `json:"protocol"`
	// 首选交互
	PreferredAction string `json:"preferredAction,omitempty"`
	// 所需权限
	Scopes []string `json:"scopes,omitempty"`
}

// PublicIntegration 是公开授权交互声明。
type PublicIntegration struct {
	// 授权提示
	Authentication *PublicAuthentication `json:"authentication,omitempty"`
}

// PublicServer 是连接器展示摘要，不提供执行配置。
type PublicServer struct {
	// 组件标识
	Key string `json:"key"`
	// 传输方式
	Type string `json:"type"`
	// 服务源站
	URL string `json:"url,omitempty"`
}

// PublicPackage 是全部资源公开出口共用的展示字段集。
type PublicPackage struct {
	// 解析契约
	ContractVersion string `json:"contractVersion"`
	// 是否声明用户可编辑配置
	HasConfiguration bool `json:"hasConfiguration"`
	// 元数据
	Manifest PublicManifest `json:"manifest"`
	// 包格式
	Format string `json:"format"`
	// 展示资料
	Presentation Presentation `json:"presentation"`
	// 上游声明
	Upstream *UpstreamSource `json:"upstream,omitempty"`
	// 软件图标
	Icon string `json:"icon,omitempty"`
	// 插件说明
	Readme string `json:"readme,omitempty"`
	// 正文状态
	ReadmeStatus string `json:"readmeStatus,omitempty"`
	// 包含技能
	Skills []Skill `json:"skills"`
	// 包含连接器
	Servers []PublicServer `json:"servers"`
	// 授权交互
	Integration PublicIntegration `json:"integration"`
	// 兼容性说明
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Public 将完整内部声明投影到与目录公开边界一致的展示数据。
func (p Package) Public() PublicPackage {
	manifest := p.Manifest
	result := PublicPackage{ContractVersion: p.ContractVersion, Manifest: PublicManifest{Schema: manifest.Schema, Name: manifest.Name, Version: manifest.Version, Description: manifest.Description, Author: manifest.Author, Homepage: manifest.Homepage, Repository: manifest.Repository, License: manifest.License, Keywords: manifest.Keywords}, Format: p.Format, Presentation: p.Presentation, Upstream: p.Upstream, Icon: p.Icon, Readme: p.Readme, ReadmeStatus: p.ReadmeStatus, Skills: p.Skills, Servers: []PublicServer{}, Diagnostics: p.Diagnostics}
	if extension, err := ClientExtension(p); err == nil {
		result.HasConfiguration = len(extension.Config) > 0
	}
	for _, server := range p.Servers {
		endpoint := ""
		if address, err := url.Parse(server.URL); err == nil && (address.Scheme == "https" || address.Scheme == "http") && address.Host != "" {
			endpoint = address.Scheme + "://" + address.Host
		}
		result.Servers = append(result.Servers, PublicServer{Key: server.Key, Type: server.Type, URL: endpoint})
	}
	if auth := p.Integration.Authentication; auth != nil {
		result.Integration.Authentication = &PublicAuthentication{Strategy: auth.Strategy, Protocol: auth.Protocol, PreferredAction: auth.PreferredAction, Scopes: auth.Scopes}
	}
	return result
}
