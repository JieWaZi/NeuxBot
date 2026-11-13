package constants

//go:generate toolx gen enum WakerOnboardingStatus

// WakerOnboardingStatus 表示伙伴的对话建档状态。
type WakerOnboardingStatus string

const (
	// WakerOnboardingStatusUnspecified 表示已可使用的旧伙伴。
	WakerOnboardingStatusUnspecified WakerOnboardingStatus = ""
	// WakerOnboardingStatusPending 表示伙伴仍在建档。
	WakerOnboardingStatusPending WakerOnboardingStatus = "pending"
	// WakerOnboardingStatusReady 表示用户已确认且档案已保存。
	WakerOnboardingStatusReady WakerOnboardingStatus = "ready"
)
