package resourcecategory

import (
	"slices"
	"strings"
	"testing"
)

func TestNormalizeCategoryTags(t *testing.T) {
	names, err := Normalize([]string{"  开发  ", "", "写作", "开发", "  "})
	if err != nil || !slices.Equal(names, []string{"开发", "写作"}) {
		t.Fatalf("names=%v error=%v", names, err)
	}
	for _, names := range [][]string{make([]string, 51), {strings.Repeat("类", 121)}} {
		if _, err := Normalize(names); err == nil {
			t.Fatal("unbounded categories accepted")
		}
	}
}
