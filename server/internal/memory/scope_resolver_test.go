package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// fakeWakerReader 是范围属主适配器测试使用的伙伴查询替身。
type fakeWakerReader struct {
	// err 是查询返回的固定错误。
	err error
}

// Get 返回固定伙伴或错误。
func (f fakeWakerReader) Get(context.Context, string) (models.Waker, error) {
	return models.Waker{ID: "waker-a"}, f.err
}

func TestPartnerExistsResolverMapsOwnersAndNotFound(t *testing.T) {
	resolver := NewPartnerExistsResolver(fakeWakerReader{})
	present, err := resolver(context.Background(), "waker-a")
	if err != nil || !present {
		t.Fatalf("existing waker must be present: present=%v err=%v", present, err)
	}
	missing := NewPartnerExistsResolver(fakeWakerReader{err: status_error.WakerNotFound.WithParams(
		map[string]interface{}{"WakerID": "waker-missing"},
	)})
	present, err = missing(context.Background(), "waker-missing")
	if err != nil || present {
		t.Fatalf("missing partner must be absent: present=%v err=%v", present, err)
	}
}

func TestPartnerExistsResolverPreservesRepositoryErrors(t *testing.T) {
	expected := errors.New("database unavailable")
	resolver := NewPartnerExistsResolver(fakeWakerReader{err: expected})
	present, err := resolver(context.Background(), "waker-a")
	if present || !errors.Is(err, expected) {
		t.Fatalf("repository error must be preserved: present=%v err=%v", present, err)
	}
}
