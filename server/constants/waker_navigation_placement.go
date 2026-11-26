package constants

//go:generate toolx gen enum WakerNavigationPlacement

// WakerNavigationPlacement 表示拖拽后伙伴相对目标伙伴的位置。
type WakerNavigationPlacement string

const (
	// WakerNavigationPlacementBefore 将伙伴放到目标伙伴前面。
	WakerNavigationPlacementBefore WakerNavigationPlacement = "before"
	// WakerNavigationPlacementAfter 将伙伴放到目标伙伴后面。
	WakerNavigationPlacementAfter WakerNavigationPlacement = "after"
)
