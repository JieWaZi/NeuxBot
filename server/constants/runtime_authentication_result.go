package constants

//go:generate toolx gen enum RuntimeAuthenticationResult

// RuntimeAuthenticationResult 区分已有账号确认、连接确认和原生授权完成。
type RuntimeAuthenticationResult string

const (
	// RuntimeAuthenticationResultUnspecified 表示尚未取得认证结果。
	RuntimeAuthenticationResultUnspecified RuntimeAuthenticationResult = ""
	// RuntimeAuthenticationResultExistingLoginVerified 表示原生确认了原有账号连接。
	RuntimeAuthenticationResultExistingLoginVerified RuntimeAuthenticationResult = "existing_login_verified"
	// RuntimeAuthenticationResultConnectionVerified 表示原生确认了连接，未证明完成新的交互授权。
	RuntimeAuthenticationResultConnectionVerified RuntimeAuthenticationResult = "connection_verified"
	// RuntimeAuthenticationResultAuthenticationCompleted 表示原生认证原语确认完成。
	RuntimeAuthenticationResultAuthenticationCompleted RuntimeAuthenticationResult = "authentication_completed"
)
