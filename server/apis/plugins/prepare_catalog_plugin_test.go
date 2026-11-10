package plugins

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gofrs/flock"
)

func TestPrepareCatalogNestedPackageGeneratesUsableLogicalTargets(t *testing.T) {
	ctx := pluginAPIContext(t)
	source := t.TempDir()
	for name, body := range map[string]string{
		"repository.txt":                               "Complete immutable source archive",
		"plugins/demo/plugin.json":                     `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"demo","version":"1.0.0"}`,
		"plugins/demo/skills/setup/SKILL.md":           "---\nname: setup\ndescription: Prepare the demo plugin\n---\nRun scripts/verify.mjs.\n",
		"plugins/demo/skills/setup/scripts/verify.mjs": "process.stdout.write('ready');\n",
	} {
		path := filepath.Join(source, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	stage, err := packageplugin.StageSource(stagedRoot(), source, "plugins/demo")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := prepareCatalogFixture(t, ctx, stage, "", false)
	if err != nil {
		t.Fatal(err)
	}
	version, err := global.PluginControllerManager.Version(ctx, detail.ResolvedVersionID)
	if err != nil || version.Package.SourcePath != "plugins/demo" {
		t.Fatal("catalog preparation lost its nested logical root", err)
	}
	content := filepath.Join(global.Config.DataDirectory, version.ContentPath)
	if err := packageplugin.VerifyContent(content, stage.Digest); err != nil {
		t.Fatal("catalog preparation changed the full source archive", err)
	}
	if _, err := os.Stat(filepath.Join(content, "repository.txt")); err != nil {
		t.Fatal("persisted source lost its repository-level content", err)
	}
	targetsRoot := filepath.Join(global.Config.DataDirectory, "plugins", "targets")
	targets, err := os.ReadDir(targetsRoot)
	if err != nil || len(targets) == 0 {
		t.Fatalf("catalog preparation did not publish derived targets: count=%d error=%v", len(targets), err)
	}
	checkedClaude := false
	for _, target := range targets {
		if strings.HasPrefix(target.Name(), ".") {
			continue
		}
		root := filepath.Join(targetsRoot, target.Name())
		for _, name := range []string{"skills/setup/SKILL.md", "skills/setup/scripts/verify.mjs"} {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(name))); err != nil {
				t.Fatalf("derived target cannot resolve its declared setup resource %s: %v", name, err)
			}
		}
		if _, err := os.Stat(filepath.Join(root, ".claude-plugin", "plugin.json")); err == nil {
			pkg, err := packageplugin.Load(root)
			if err != nil || len(pkg.Skills) != 1 || pkg.Skills[0].Name != "setup" {
				t.Fatalf("derived Claude target does not validate at its real plugin root: %v", err)
			}
			checkedClaude = true
		}
	}
	if !checkedClaude {
		t.Fatal("catalog preparation did not publish a usable Claude entry")
	}
}

func TestPreparingTargetsDoesNotHoldContentAdmissionGuard(t *testing.T) {
	ctx := pluginAPIContext(t)
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "plugin.json"), []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"parallel","version":"1.0.0"}`), 0600); err != nil {
		t.Fatal(err)
	}
	stage, err := packageplugin.Stage(stagedRoot(), source)
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(global.Config.DataDirectory, "plugins", "targets")
	if _, err := packageplugin.PrepareTargets(t.Context(), parent, "parallel", stage.Package, stage.Root); err != nil {
		t.Fatal(err)
	}
	locks, err := os.ReadDir(filepath.Join(parent, ".locks"))
	if err != nil || len(locks) == 0 {
		t.Fatal("target generation did not create publication locks", err)
	}
	held := []*flock.Flock{}
	for _, entry := range locks {
		lock := flock.New(filepath.Join(parent, ".locks", entry.Name()))
		if err := lock.Lock(); err != nil {
			t.Fatal(err)
		}
		held = append(held, lock)
	}
	unlock := sync.OnceFunc(func() {
		for _, lock := range held {
			_ = lock.Close()
		}
	})
	defer unlock()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			if err := os.RemoveAll(filepath.Join(parent, entry.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
	content, err := packageplugin.CopyContent(filepath.Join(global.Config.DataDirectory, "plugins", "content"), stage.Root, stage.Digest)
	if err != nil {
		t.Fatal(err)
	}
	previousContents := global.PluginContents
	t.Cleanup(func() { global.PluginContents = previousContents })
	contents := packageplugin.NewContentStore(global.Config.DataDirectory, func() ([]string, error) { return []string{content}, nil })
	global.PluginContents = contents
	queried := make(chan struct{}, 8)
	global.PluginControllerManager = observedSourceController{PluginController: global.PluginControllerManager, queried: queried}
	prepared := make(chan error, 1)
	go func() {
		_, _, err := persistPreparedResource(ctx, stage, false, models.Plugin{ID: "parallel", Name: "parallel", SourceKey: "catalog:parallel"}, nil)
		prepared <- err
	}()
	<-queried
	admitted := make(chan error, 1)
	go func() {
		release, err := contents.Acquire(content)
		if err == nil {
			release()
		}
		admitted <- err
	}()
	blocked := false
	select {
	case err := <-admitted:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(time.Second):
		blocked = true
	}
	unlock()
	if err := <-prepared; err != nil {
		t.Error(err)
	}
	if blocked {
		if err := <-admitted; err != nil {
			t.Error(err)
		}
		t.Fatal("target publication blocked an unrelated content lease")
	}
}

// observedSourceController 保留真实仓储查询，仅用于同步资源准备的并发窗口。
type observedSourceController struct {
	// PluginController 是实际数据库控制器。
	controllers.PluginController
	// queried 通知来源查询已经完成，不替代真实查询结果。
	queried chan struct{}
}

// FindSource 完成真实来源查询后通知测试中的并发准备边界。
func (c observedSourceController) FindSource(ctx context.Context, source string) (models.Plugin, error) {
	resource, err := c.PluginController.FindSource(ctx, source)
	c.queried <- struct{}{}
	return resource, err
}

func TestPreparedResourceResolvesStableIdentityInsideContentBoundary(t *testing.T) {
	ctx := pluginAPIContext(t)
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "plugin.json"), []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"stable","version":"1.0.0"}`), 0600); err != nil {
		t.Fatal(err)
	}
	stage, err := packageplugin.Stage(stagedRoot(), source)
	if err != nil {
		t.Fatal(err)
	}
	previousContents := global.PluginContents
	t.Cleanup(func() { global.PluginContents = previousContents })
	global.PluginContents = packageplugin.NewContentStore(global.Config.DataDirectory, nil)
	store := global.PluginControllerManager
	queried := make(chan struct{}, 8)
	global.PluginControllerManager = observedSourceController{PluginController: store, queried: queried}
	release := sync.OnceFunc(global.PluginContents.Guard())
	defer release()
	completed := make(chan error, 1)
	go func() {
		_, _, err := persistPreparedResource(ctx, stage, false, models.Plugin{ID: "temporary-id", Name: "stable", SourceKey: "catalog:stable"}, nil)
		completed <- err
	}()
	lookupBeforeBoundary := false
	select {
	case <-queried:
		lookupBeforeBoundary = true
	case <-time.After(100 * time.Millisecond):
	}
	if err := store.Create(ctx, models.Plugin{ID: "settled-id", Name: "stable", SourceKey: "catalog:stable"}); err != nil {
		t.Fatal(err)
	}
	release()
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	if lookupBeforeBoundary {
		t.Fatal("resource preparation selected its identity before owning the content boundary")
	}
	parent := filepath.Join(global.Config.DataDirectory, "plugins", "targets")
	before, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := packageplugin.PrepareTargets(t.Context(), parent, "settled-id", stage.Package, stage.Root); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadDir(parent)
	if err != nil || len(before) == 0 || len(after) != len(before) {
		t.Fatalf("prepared targets did not use the committed identity: before=%d after=%d error=%v", len(before), len(after), err)
	}
}
