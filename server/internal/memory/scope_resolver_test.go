package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
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

// fakeProjectReader 是范围属主适配器测试使用的项目查询替身。
type fakeProjectReader struct {
	// err 是查询返回的固定错误。
	err error
}

// Get 返回固定项目或错误。
func (f fakeProjectReader) Get(context.Context, string) (models.Project, error) {
	return models.Project{ID: "project-a"}, f.err
}

func TestScopeExistsResolverMapsOwnersAndNotFound(t *testing.T) {
	resolver := NewScopeExistsResolver(fakeWakerReader{}, fakeProjectReader{err: status_error.ProjectNotFound})
	present, err := resolver(context.Background(), constants.MemoryScopePartner, "waker-a")
	if err != nil || !present {
		t.Fatalf("existing waker must be present: present=%v err=%v", present, err)
	}
	present, err = resolver(context.Background(), constants.MemoryScopeProject, "project-missing")
	if err != nil || present {
		t.Fatalf("missing project must be absent: present=%v err=%v", present, err)
	}
}

func TestScopeExistsResolverPreservesRepositoryErrors(t *testing.T) {
	expected := errors.New("database unavailable")
	resolver := NewScopeExistsResolver(fakeWakerReader{err: expected}, fakeProjectReader{})
	present, err := resolver(context.Background(), constants.MemoryScopePartner, "waker-a")
	if present || !errors.Is(err, expected) {
		t.Fatalf("repository error must be preserved: present=%v err=%v", present, err)
	}
}
