package plugins

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestPrepareCatalogKeepsOnlyTheVerifiedSource(t *testing.T) {
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
	if _, err := os.Stat(targetsRoot); !os.IsNotExist(err) {
		t.Fatal("preparation materialized derived targets before any runtime was selected", err)
	}
	pluginRoot, err := packageplugin.PackageRoot(content, version.Package)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := packageplugin.ContentDigest(pluginRoot)
	if err != nil {
		t.Fatal(err)
	}
	target, release, err := packageplugin.PrepareRuntimeTarget(t.Context(), targetsRoot, version.PluginID, version.Package, pluginRoot, "claude", digest)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for _, name := range []string{".claude-plugin/plugin.json", "skills/setup/SKILL.md", "skills/setup/scripts/verify.mjs"} {
		if _, err := os.Stat(filepath.Join(target.Root, filepath.FromSlash(name))); err != nil {
			t.Fatalf("derived target lost its declared resource %s: %v", name, err)
		}
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
	resource, err := store.FindSource(ctx, "catalog:stable")
	if err != nil || resource.ID != "settled-id" {
		t.Fatalf("preparation did not use the committed resource identity: %+v %v", resource, err)
	}
	versions, err := store.Versions(ctx, resource.ID)
	if err != nil || len(versions) != 1 || versions[0].Digest != stage.Digest {
		t.Fatalf("prepared content was not attached to the settled identity: %+v %v", versions, err)
	}
}
