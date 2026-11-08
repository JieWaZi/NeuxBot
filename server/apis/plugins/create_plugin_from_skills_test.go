package plugins

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	skillapi "github.com/JieWaZi/NeuxBot/server/apis/skills"
	"github.com/JieWaZi/NeuxBot/server/apis/uploads"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	internalskills "github.com/JieWaZi/NeuxBot/server/internal/skills"
	uploadstore "github.com/JieWaZi/NeuxBot/server/internal/uploads"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func skillBundleAPIContext(t *testing.T) *gin.Context {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "skill-bundle.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sql, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	previousPlugins, previousSkills, previousWakers := global.PluginControllerManager, global.SkillControllerManager, global.WakerControllerManager
	previousTx, previousData, previousContents, previousUploads := global.TransactionControllerManager, global.Config.DataDirectory, global.PluginContents, global.Uploads
	t.Cleanup(func() {
		global.PluginControllerManager, global.SkillControllerManager, global.WakerControllerManager = previousPlugins, previousSkills, previousWakers
		global.TransactionControllerManager, global.Config.DataDirectory, global.PluginContents, global.Uploads = previousTx, previousData, previousContents, previousUploads
		_ = sql.Close()
	})
	global.PluginControllerManager = controllers.NewPluginController(database.DB)
	global.SkillControllerManager = controllers.NewSkillController(database.DB)
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
	global.Config.DataDirectory, global.PluginContents, global.Uploads = t.TempDir(), nil, nil
	t.Setenv("ALLY_PLUGIN_CATALOG_URL", t.TempDir())
	ctx, _ := gin.CreateTestContext(nil)
	for _, id := range []string{"w1", "w2"} {
		if err := global.WakerControllerManager.Create(ctx, models.Waker{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	return ctx
}

func skillBundleArchive(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	for path, body := range map[string]string{
		"review/SKILL.md":            "---\nname: review\ndescription: Review real changes\n---\nRead references/rules.md\n",
		"review/references/rules.md": "Actual review resource", "review/scripts/run.sh": "#!/bin/sh\ntrue\n",
		"tests/SKILL.md":  "---\nname: tests\ndescription: Run real tests\n---\nTest the project.\n",
		"unused/SKILL.md": "---\nname: unused\ndescription: Not selected\n---\nNot selected.\n",
		"LICENSE":         "Original source license", "mcp.json": "Not part of the selected skills", ".neuxbot/auth.mjs": "Not part of the selected skills",
	} {
		entry, err := writer.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func stageSkillBundleBatch(t *testing.T, ctx *gin.Context) skillapi.SkillImportBatch {
	t.Helper()
	archive := filepath.Join(t.TempDir(), "skills.zip")
	if err := os.WriteFile(archive, skillBundleArchive(t), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := (&skillapi.ScanLibrarySkillImport{Body: skillapi.ScanSkillImportRequest{Kind: constants.SkillSourceZip, Location: archive}}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return value.(skillapi.SkillImportBatch)
}

func TestCreatePluginFromSkillsConsumesBatchAndKeepsLibraryBoundaries(t *testing.T) {
	ctx := skillBundleAPIContext(t)
	batch := stageSkillBundleBatch(t, ctx)
	value, err := (&CreatePluginFromSkills{Body: CreatePluginFromSkillsRequest{Token: batch.Token, CandidateIDs: []string{"review", "tests"}, Name: "  审阅与测试  ", Description: "实际扫描技能"}}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	detail := value.(PluginDetailResponse)
	if detail.Plugin.Name != "审阅与测试" || len(detail.Installations) != 0 || len(detail.Versions) != 1 || len(detail.Versions[0].Package.Skills) != 2 || detail.Plugin.SourceKey != constants.PluginLocalSkillSourcePrefix+detail.Plugin.ID {
		t.Fatalf("wrong created plugin: %+v", detail)
	}
	if _, err := os.Stat(filepath.Join(global.Config.DataDirectory, "skills/tmp", batch.Token)); !os.IsNotExist(err) {
		t.Fatal("successful create did not consume batch")
	}
	_, total, err := global.SkillControllerManager.ListLibrary(ctx, "", 0, 100)
	if err != nil || total != 0 {
		t.Fatal("plugin skills entered independent library", total, err)
	}
	browse := readBrowse(t, ctx, BrowsePlugins{})
	if len(browse.Entries) != 1 || browse.Entries[0].PluginID != detail.Plugin.ID || browse.Entries[0].VersionID != detail.ResolvedVersionID || browse.Entries[0].InstalledCount != 0 || browse.InstalledTotalSize != 0 || browse.TotalSize != nil {
		t.Fatalf("uninstalled local package disappeared or was marked installed: %+v", browse)
	}
	if installed := readBrowse(t, ctx, BrowsePlugins{InstalledOnly: true}); len(installed.Entries) != 0 || installed.InstalledTotalSize != 0 {
		t.Fatal("local library entry counted as installed")
	}
	browseCatalog(t, "catalog-only")
	browseFixture(t, ctx, "actual-installed", "old", true)
	browse = readBrowse(t, ctx, BrowsePlugins{PageSize: 3})
	if browse.TotalSize == nil || *browse.TotalSize != 3 || browse.InstalledTotalSize != 1 || len(browse.Entries) != 3 || browse.Entries[0].PluginID != "actual-installed" || browse.Entries[1].PluginID != detail.Plugin.ID || browse.Entries[2].CatalogKey != "catalog-only" {
		t.Fatalf("installed/local/catalog page mismatch: %+v", browse)
	}
}

func TestCreatePluginFromSkillsRejectsInvalidSelectionAndChangedBatch(t *testing.T) {
	ctx := skillBundleAPIContext(t)
	batch := stageSkillBundleBatch(t, ctx)
	for _, ids := range [][]string{{"review"}, {"review", "review"}, {"review", "missing"}} {
		_, err := (&CreatePluginFromSkills{Body: CreatePluginFromSkillsRequest{Token: batch.Token, CandidateIDs: ids, Name: "Bundle"}}).Output(ctx)
		if !errors.Is(err, status_error.BadRequest) {
			t.Fatalf("invalid selection accepted: %v", err)
		}
	}
	if _, err := internalskills.OpenStagedImport(filepath.Join(global.Config.DataDirectory, "skills/tmp"), batch.Token); err != nil {
		t.Fatal("selection failure consumed batch", err)
	}
	if err := os.WriteFile(filepath.Join(global.Config.DataDirectory, "skills/tmp", batch.Token, "content/review/references/rules.md"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (&CreatePluginFromSkills{Body: CreatePluginFromSkillsRequest{Token: batch.Token, CandidateIDs: []string{"review", "tests"}, Name: "Bundle"}}).Output(ctx); !errors.Is(err, status_error.SkillImportIntegrityInvalid) {
		t.Fatal("changed scan was accepted", err)
	}
	_, total, err := global.PluginControllerManager.List(ctx, "", "", 0, 100)
	if err != nil || total != 0 {
		t.Fatal("failed selection wrote a plugin", total, err)
	}
}

func TestSkillBundleHTTPProcess(t *testing.T) {
	if os.Getenv("NEUX_SKILL_BUNDLE_HTTP") != "1" {
		return
	}
	skillBundleAPIContext(t)
	global.Config.Security.LocalToken = "skill-bundle-test"
	var err error
	global.Uploads, err = uploadstore.New(global.Config.DataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer global.Uploads.Close()
	port, err := strconv.Atoi(os.Getenv("NEUX_SKILL_BUNDLE_PORT"))
	if err != nil {
		t.Fatal(err)
	}
	root := ginx.NewRouter(ginx.Group("/api/v1"), &middleware.LocalTokenAuth{})
	root.Register(Router)
	root.Register(WakerRouter)
	root.Register(skillapi.LibraryRouter)
	root.Register(uploads.Router)
	ginx.RunServer(&conf.Server{Host: "127.0.0.1", Port: port, ExitWaitTimeout: 1, Log: &conf.Log{ToStdout: true}}, root)
}

func TestSkillBundleProductionHTTPLifecycle(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "-test.run=^TestSkillBundleHTTPProcess$")
	command.Env = append(os.Environ(), "NEUX_SKILL_BUNDLE_HTTP=1", fmt.Sprintf("NEUX_SKILL_BUNDLE_PORT=%d", port), "TMPDIR="+t.TempDir())
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	base := fmt.Sprintf("http://127.0.0.1:%d/api/v1", port)
	client := http.Client{Timeout: 15 * time.Second}
	for deadline := time.Now().Add(15 * time.Second); ; {
		connection, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
		if err == nil {
			_ = connection.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("production plugin router did not start")
		}
		time.Sleep(30 * time.Millisecond)
	}
	request := func(method, path string, body any, result any) {
		t.Helper()
		var reader io.Reader
		if raw, ok := body.([]byte); ok {
			reader = bytes.NewReader(raw)
		} else if body != nil {
			encoded, _ := json.Marshal(body)
			reader = bytes.NewReader(encoded)
		}
		req, err := http.NewRequest(method, base+path, reader)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer skill-bundle-test")
		req.Header.Set("Content-Type", "application/json")
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
			t.Fatalf("%s %s: HTTP %d %s", method, path, response.StatusCode, body)
		}
		if result != nil {
			if err := json.NewDecoder(response.Body).Decode(result); err != nil {
				t.Fatal(err)
			}
		}
	}
	var upload uploads.UploadResponse
	request(http.MethodPost, "/uploads?name=skills.zip", skillBundleArchive(t), &upload)
	var batch skillapi.SkillImportBatch
	request(http.MethodPost, "/skills/import/scan", skillapi.ScanSkillImportRequest{Kind: constants.SkillSourceZip, Location: upload.SourcePath}, &batch)
	if len(batch.Candidates) != 3 {
		t.Fatal("scan did not discover actual skills")
	}
	var detail PluginDetailResponse
	request(http.MethodPost, "/plugins/from-skills", CreatePluginFromSkillsRequest{Token: batch.Token, CandidateIDs: []string{"review", "tests"}, Name: "审阅与测试", Description: "真实资源"}, &detail)
	if len(detail.Installations) != 0 || len(detail.Versions) != 1 || len(detail.Versions[0].Package.Skills) != 2 {
		t.Fatal("create implicitly installed or omitted selected skills")
	}
	var browse BrowsePluginsResponse
	request(http.MethodGet, "/plugins/browse", nil, &browse)
	if len(browse.Entries) != 1 || browse.InstalledTotalSize != 0 || browse.Entries[0].VersionID != detail.ResolvedVersionID {
		t.Fatal("created library plugin not visible")
	}
	var library skillapi.ListLibrarySkillsResponse
	request(http.MethodGet, "/skills", nil, &library)
	if library.TotalSize != 0 {
		t.Fatal("plugin components entered independent skills library")
	}
	var installed InstallPluginResponse
	request(http.MethodPost, "/plugins/"+detail.Plugin.ID+"/installations", InstallPluginRequest{VersionID: detail.ResolvedVersionID, WakerIDs: []string{"w1"}}, &installed)
	if len(installed.Results) != 1 || !installed.Results[0].Installed {
		t.Fatalf("explicit local plugin installation failed: %+v", installed)
	}
	var skill PluginSkillResponse
	request(http.MethodGet, "/plugins/"+detail.Plugin.ID+"/skill?versionId="+detail.ResolvedVersionID+"&wakerId=w1&key="+url.QueryEscape("skills/review")+"&path="+url.QueryEscape("references/rules.md"), nil, &skill)
	if skill.File == nil || skill.File.Content != "Actual review resource" || !strings.Contains(skill.Content, "Read references/rules.md") {
		t.Fatal("real plugin skill and resource unavailable")
	}
	request(http.MethodPut, "/wakers/w1/plugins/"+detail.Plugin.ID+"/enabled", PluginEnabledRequest{Enabled: false}, nil)
	request(http.MethodGet, "/plugins/"+detail.Plugin.ID, nil, &detail)
	if len(detail.Installations) != 1 || detail.Installations[0].Enabled {
		t.Fatal("plugin disable did not persist")
	}
	request(http.MethodDelete, "/wakers/w1/plugins/"+detail.Plugin.ID, nil, nil)
	browse = BrowsePluginsResponse{}
	request(http.MethodGet, "/plugins/browse", nil, &browse)
	if len(browse.Entries) != 1 || browse.InstalledTotalSize != 0 || browse.Entries[0].VersionID == "" || browse.Entries[0].InstalledCount != 0 {
		t.Fatal("last uninstall removed the local library resource")
	}
	request(http.MethodGet, "/skills", nil, &library)
	if library.TotalSize != 0 {
		t.Fatal("plugin lifecycle wrote independent skills")
	}
	t.Log("production HTTP + SQLite: upload -> scan -> create -> browse -> explicit install -> real skill/resource read -> disable -> uninstall; independent skill library remains empty")
}
