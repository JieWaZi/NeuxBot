package accountconfig

import (
	"slices"
	"testing"
)

func TestEnvironmentOmitsACPAccountOverrides(t *testing.T) {
	inherited := []string{
		"PATH=/fixture/bin", "HTTPS_PROXY=https://proxy.example.invalid", "CODEBUDDY_CONFIG_DIR=/native",
		"qoder_personal_access_token=fixture", "QODER_WORKING_DIR=/other",
		"WORKBUDDY_CONFIG_DIR=/desktop", "QODERCN_PERSONAL_ACCESS_TOKEN=cn-fixture", "ACC_PRODUCT_CONFIG_PATH=/other",
	}
	if got := Environment(inherited); !slices.Equal(got, inherited[:2]) {
		t.Fatal("the ordinary tool environment inherited an ACP account override")
	}
}
