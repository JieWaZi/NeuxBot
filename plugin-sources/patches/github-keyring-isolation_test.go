package config

import (
	"testing"

	systemkeyring "github.com/zalando/go-keyring"
)

// 验证官方真实登录编排在应用内执行时，不读写外部 gh 的活跃和同名用户凭据。
// newTestAuthConfig 使用上游内存钥匙串替身，不访问测试机器的真实系统存储。
func TestNeuxAllyLoginKeepsExternalKeyringUntouched(t *testing.T) {
	auth := newTestAuthConfig(t)
	for user, token := range map[string]string{"": "external-active", "same-user": "external-user"} {
		if err := systemkeyring.Set("gh:github.com", user, token); err != nil {
			t.Fatal(err)
		}
	}
	insecure, err := auth.Login("github.com", "same-user", "managed-token", "https", false)
	if err != nil || !insecure {
		t.Fatalf("managed login failed: insecure=%v error=%v", insecure, err)
	}
	for user, wanted := range map[string]string{"": "external-active", "same-user": "external-user"} {
		got, err := systemkeyring.Get("gh:github.com", user)
		if err != nil || got != wanted {
			t.Fatalf("external keyring changed for user %q: got=%q error=%v", user, got, err)
		}
	}
	token, source := auth.ActiveToken("github.com")
	if token != "managed-token" || source != "oauth_token" {
		t.Fatalf("private login not authoritative: token=%q source=%s", token, source)
	}
}
