package builtinwaker

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

type wakerCatalogFake struct {
	// wakers 保存测试中的已创建伙伴。
	wakers map[string]models.Waker
}

func (fake *wakerCatalogFake) Create(_ context.Context, waker models.Waker) error {
	fake.wakers[waker.ID] = waker
	return nil
}

func (fake *wakerCatalogFake) Get(_ context.Context, wakerID string) (models.Waker, error) {
	waker, exists := fake.wakers[wakerID]
	if !exists {
		return models.Waker{}, status_error.WakerNotFound
	}
	return waker, nil
}

func TestEnsureDefaultsSeedsPersonalAssistantWaker(t *testing.T) {
	wakers := &wakerCatalogFake{wakers: make(map[string]models.Waker)}
	if err := EnsureDefaults(context.Background(), wakers); err != nil {
		t.Fatalf("EnsureDefaults() error = %v", err)
	}
	waker, exists := wakers.wakers[builtinrolepreset.PersonalAssistantWakerID]
	if !exists {
		t.Fatal("missing personal assistant waker")
	}
	if waker.Avatar != constants.PersonalAssistantAvatar {
		t.Fatalf("avatar = %q", waker.Avatar)
	}
	if waker.Name != "lumi" || waker.RoleName != "私人助理" {
		t.Fatalf("unexpected waker: %+v", waker)
	}
}

func TestEnsureDefaultsSkipsWhenBuiltinWakerAlreadyExists(t *testing.T) {
	wakers := &wakerCatalogFake{
		wakers: map[string]models.Waker{
			builtinrolepreset.PersonalAssistantWakerID: {
				ID:     builtinrolepreset.PersonalAssistantWakerID,
				Name:   "私人助理",
				Avatar: "companion:v1:0123456789abcdef0123456789abcdef",
			},
		},
	}
	if err := EnsureDefaults(context.Background(), wakers); err != nil {
		t.Fatalf("EnsureDefaults() error = %v", err)
	}
	if wakers.wakers[builtinrolepreset.PersonalAssistantWakerID].Avatar != "companion:v1:0123456789abcdef0123456789abcdef" {
		t.Fatal("startup must preserve an existing companion appearance")
	}
	if len(wakers.wakers) != len(builtinrolepreset.DefinitionsWithDefaultWaker()) {
		t.Fatalf("waker count = %d, want all registered built-ins", len(wakers.wakers))
	}
}
