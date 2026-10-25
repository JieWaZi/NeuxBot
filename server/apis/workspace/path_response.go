package workspace

// PathResponse 是目录或导入操作后的资源位置。
type PathResponse struct {
	// 相对路径
	RelativePath string `json:"relativePath"`
}
