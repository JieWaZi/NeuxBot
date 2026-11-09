package runtimeenvironment

import (
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// startupFailingRepository 模拟探测结果无法持久化，保留真实临时数据库的其他行为。
type startupFailingRepository struct {
	// RuntimeEnvironmentController 承载初始化、查询与默认选择的真实仓储。
	controllers.RuntimeEnvironmentController
}

// UpdateDetection 拒绝探测提交，验证故障不会被解释成没有 CLI。
func (r startupFailingRepository) UpdateDetection(context.Context, string, agentapi.DetectionResult, int64) error {
	return status_error.DatabaseError
}

// TestStartupAdmissionUsesPersistedEvidence 验证启动准入先冻结数据库证据，后台完成后才结束等待并发出事件。
func TestStartupAdmissionUsesPersistedEvidence(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(map[bool]string{false: "新环境", true: "已有安装"}[cached], func(t *testing.T) {
			base, repo, api := newRuntimeEnvironmentTestService(t)
			svc := api.(*service)
			ctx := context.Background()
			if _, err := svc.EnsureInitialized(ctx); err != nil {
				t.Fatal(err)
			}
			if cached {
				if err := repo.UpdateDetection(ctx, agentapi.RuntimeIDCodex, agentapi.DetectionResult{ResolvedBinary: "/usr/local/bin/codex"}, 1); err != nil {
					t.Fatal(err)
				}
			}
			release := make(chan struct{})
			catalog := &controlledRuntimeCatalog{countingRuntimeCatalog: base, version: "1.2.3", started: make(chan string, 2), blockedRuntime: agentapi.RuntimeIDCodex, release: release}
			svc.runtimeCatalog = catalog
			svc.probeProvider = catalog.probe
			hub := events.New()
			defer hub.Close()
			subscription := hub.Subscribe("runtimes", events.Resource{Kind: "global"})
			defer subscription.Close()
			svc.eventPublisher = hub
			if err := svc.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer svc.Close()
			pending, failed, required := svc.StartupDetection()
			if !pending || failed || required == cached {
				close(release)
				t.Fatalf("startup = %v %v %v", pending, failed, required)
			}
			close(release)
			select {
			case event := <-subscription.Events:
				pending, failed, required = svc.StartupDetection()
				if pending || failed || required == cached || event.Type != "runtimecontract.changed" {
					t.Fatalf("terminal = %v %v %v %+v", pending, failed, required, event)
				}
				svc.CompleteStartupSetup()
				_, _, required = svc.StartupDetection()
				if required {
					t.Fatal("explicit default selection did not release startup admission")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("startup completion event missing")
			}
		})
	}
}

// TestStartupDetectionFailureIsExplicit 验证持久化故障结束等待，且后续权威查询能够区别未安装与失败。
func TestStartupDetectionFailureIsExplicit(t *testing.T) {
	_, repo, api := newRuntimeEnvironmentTestService(t)
	svc := api.(*service)
	svc.environments = startupFailingRepository{repo}
	hub := events.New()
	defer hub.Close()
	subscription := hub.Subscribe("runtimes", events.Resource{Kind: "global"})
	defer subscription.Close()
	svc.eventPublisher = hub
	if err := svc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	select {
	case <-subscription.Events:
		pending, failed, required := svc.StartupDetection()
		if pending || !failed || !required {
			t.Fatalf("failed startup = %v %v %v", pending, failed, required)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("failed startup did not wake subscriber")
	}
}

// TestManualRefreshRecoversStartupFailure 验证失败重试保留故障，完整重检成功后恢复准入并通知订阅者。
func TestManualRefreshRecoversStartupFailure(t *testing.T) {
	_, repo, api := newRuntimeEnvironmentTestService(t)
	svc := api.(*service)
	svc.environments = startupFailingRepository{repo}
	hub := events.New()
	defer hub.Close()
	subscription := hub.Subscribe("runtimes", events.Resource{Kind: "global"})
	defer subscription.Close()
	svc.eventPublisher = hub
	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	select {
	case <-subscription.Events:
	case <-time.After(5 * time.Second):
		t.Fatal("startup failure event missing")
	}
	// 等待后台入口彻底结束，再恢复仓储，避免测试替换依赖与后台读取竞态。
	svc.waitGroup.Wait()
	if err := svc.RefreshAll(ctx); err == nil {
		t.Fatal("retry unexpectedly succeeded")
	}
	if pending, failed, required := svc.StartupDetection(); pending || !failed || !required {
		t.Fatalf("failed retry lost startup failure: %v %v %v", pending, failed, required)
	}
	svc.environments = repo
	if err := svc.RefreshAll(ctx); err != nil {
		t.Fatal(err)
	}
	if pending, failed, required := svc.StartupDetection(); pending || failed || !required {
		t.Fatalf("successful refresh did not recover startup failure: %v %v %v", pending, failed, required)
	}
	select {
	case <-subscription.Events:
	case <-time.After(time.Second):
		t.Fatal("recovery did not wake subscriber")
	}
	svc.CompleteStartupSetup()
	if _, failed, required := svc.StartupDetection(); failed || required {
		t.Fatal("completed setup still blocks renderer reload")
	}
}

// countingRuntimeCatalog 记录通过服务高层 seam 发生的探测次数。
type countingRuntimeCatalog struct {
	// summaries 覆盖测试要对账的内置 Runtime；空值沿用 Codex 和 Claude。
	summaries []agentapi.RuntimeSummary
	// mutex 保护并发探测次数和 Runtime 标识记录。
	mutex sync.Mutex
	// detections 保存每次 Detect 收到的 Runtime 标识。
	detections []string
	// requests 保存每次 Detect 收到的完整启动覆盖请求。
	requests []agentapi.DetectRequest
	// permissionLevels 是写入探测快照的产品权限；nil 表示模型未声明权限。
	permissionLevels []agentapi.PermissionLevel
	// codexOptionsUnavailable 控制 Codex 探测是否模拟模型目录刷新失败。
	codexOptionsUnavailable bool
}

// DiscoverInstallations 返回测试环境中的安装路径快照。
func (e *countingRuntimeCatalog) DiscoverInstallations(
	_ context.Context,
	request agentapi.DetectRequest,
	previous []agentapi.CLIInstallation,
) []agentapi.CLIInstallation {
	result := append([]agentapi.CLIInstallation(nil), previous...)
	if len(result) == 0 {
		result = append(result, agentapi.CLIInstallation{
			Path: "/usr/local/bin/" + request.RuntimeID, Available: true,
		})
	}
	return result
}

func TestRefreshKeepsSelectedInstallationWhenPathOrderChanges(t *testing.T) {
	catalog := &countingRuntimeCatalog{}
	svc := &service{runtimeCatalog: catalog, probeProvider: catalog.probe}
	summary := catalog.Runtimes()[1]
	svc.detect(context.Background(), summary, models.RuntimeEnvironment{
		RuntimeID: agentapi.RuntimeIDCodex,
		Binary:    "codex",
		Detection: agentapi.DetectionResult{
			ResolvedBinary: "/custom/bin/codex",
			Installations: []agentapi.CLIInstallation{
				{Path: "/custom/bin/codex", Version: "2.0", Available: true},
				{Path: "/usr/local/bin/codex", Version: "1.0", Available: true},
			},
		},
	})
	if got := catalog.lastRequest(agentapi.RuntimeIDCodex).LaunchOverride.Binary; got != "/custom/bin/codex" {
		t.Fatalf("refresh selected %q, want persisted path", got)
	}
}

// Runtimes 返回测试使用的两个内置 Runtime 摘要。
func (e *countingRuntimeCatalog) Runtimes() []agentapi.RuntimeSummary {
	if e.summaries != nil {
		return e.summaries
	}
	return []agentapi.RuntimeSummary{
		{
			ID:                            agentapi.RuntimeIDClaude,
			BaseRuntimeID:                 agentapi.RuntimeIDClaude,
			Name:                          "Claude Code",
			BinaryCandidates:              []string{"claude"},
			DefaultModel:                  "sonnet",
			ConfigHomeEnvironmentVariable: "CLAUDE_CONFIG_DIR",
		},
		{
			ID:               agentapi.RuntimeIDCodex,
			BaseRuntimeID:    agentapi.RuntimeIDCodex,
			Name:             "Codex CLI",
			BinaryCandidates: []string{"codex"},
			DefaultModel:     "gpt-stable",
			ReasoningOptions: []agentapi.ReasoningOption{
				{ID: "high", Name: "High"},
			},
			ConfigHomeEnvironmentVariable: "CODEX_HOME",
		},
	}
}

// ResolveExecutable 模拟独立于账号认证的 CLI 安装检查。
func (e *countingRuntimeCatalog) ResolveExecutable(request agentapi.DetectRequest) (string, error) {
	return "/usr/local/bin/" + request.RuntimeID, nil
}

// TestRefreshPinsDefaultAndPreservesConfiguredPiCommand 验证默认路径固定及自定义命令保留。
func TestRefreshPinsDefaultAndPreservesConfiguredPiCommand(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "pi.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	environments := controllers.NewRuntimeEnvironmentController(database.DB)
	catalog := &countingRuntimeCatalog{summaries: []agentapi.RuntimeSummary{{ID: agentapi.RuntimeIDPi, BaseRuntimeID: agentapi.RuntimeIDPi, Name: "Pi", BinaryCandidates: []string{"pi"}}}}
	svc, err := newWithRuntimeCatalog(catalog, environments, controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB)))
	if err != nil {
		t.Fatal(err)
	}
	svc.(*service).probeProvider = catalog.probe
	ctx := context.Background()
	if _, err := svc.EnsureInitialized(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshAll(ctx); err != nil {
		t.Fatal(err)
	}
	initial, err := environments.Get(ctx, "pi")
	if err != nil {
		t.Fatal(err)
	}
	if initial.Binary != "/usr/local/bin/pi" {
		t.Fatalf("default installation path = %q, want pinned executable", initial.Binary)
	}
	if err := environments.UpdateFields(ctx, "pi", map[string]any{"binary": "pi-acp", "name": "Pi ACP"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshAll(ctx); err != nil {
		t.Fatal(err)
	}
	actual, err := environments.Get(ctx, "pi")
	if err != nil {
		t.Fatal(err)
	}
	if actual.Binary != "pi-acp" || actual.Name != "Pi" || catalog.lastRequest("pi").LaunchOverride.Binary != "pi-acp" {
		t.Fatalf("Pi command was rewritten: %+v", actual)
	}
	if err := environments.UpdateFields(ctx, "pi", map[string]any{"binary": "/opt/tools/pi"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshOne(ctx, "pi"); err != nil {
		t.Fatal(err)
	}
	if catalog.lastRequest("pi").LaunchOverride.Binary != "/opt/tools/pi" {
		t.Fatal("custom Pi command was replaced")
	}
}

// Detect 返回可直接持久化和执行准备使用的就绪快照。
func (e *countingRuntimeCatalog) probe(
	_ context.Context,
	summary agentapi.RuntimeSummary,
	_ models.RuntimeEnvironment,
	request agentapi.DetectRequest,
) agentapi.DetectionResult {
	e.mutex.Lock()
	e.detections = append(e.detections, request.RuntimeID)
	e.requests = append(e.requests, request)
	codexOptionsUnavailable := e.codexOptionsUnavailable
	permissionLevels := append([]agentapi.PermissionLevel(nil), e.permissionLevels...)
	e.mutex.Unlock()
	result := agentapi.DetectionResult{
		Runtime:        summary,
		State:          agentapi.DetectionStateReady,
		ResolvedBinary: "/usr/local/bin/" + request.RuntimeID,
		Models: []agentapi.ModelOption{{
			ID:               "sonnet",
			Name:             "Sonnet",
			PermissionLevels: append([]agentapi.PermissionLevel(nil), permissionLevels...),
		}},
		DetectedDefaultModel: "sonnet",
		Capabilities: agentapi.CapabilitySet{
			agentapi.CapabilityStructuredOutput: true,
			agentapi.CapabilityDynamicModels:    request.RuntimeID == agentapi.RuntimeIDCodex && !codexOptionsUnavailable,
		},
	}
	if request.RuntimeID == agentapi.RuntimeIDCodex {
		if codexOptionsUnavailable {
			result.Diagnostics = []agentapi.Diagnostic{{
				Code:    "ACP_OPTIONS_UNAVAILABLE",
				Level:   agentapi.DiagnosticLevelWarning,
				Message: "timeout waiting for child process to exit",
			}}
			return result
		}
		result.Models = []agentapi.ModelOption{{
			ID:               "gpt-stable",
			Name:             "GPT Stable",
			PermissionLevels: append([]agentapi.PermissionLevel(nil), permissionLevels...),
			ReasoningOptions: []agentapi.ReasoningOption{
				{ID: "high", Name: "High"},
				{ID: "medium", Name: "Medium"},
			},
		}}
		result.ReasoningOptions = []agentapi.ReasoningOption{
			{ID: "high", Name: "High"},
			{ID: "medium", Name: "Medium"},
		}
		result.DetectedDefaultModel = "gpt-stable"
		result.DetectedDefaultReasoning = "high"
	}
	return result
}

// detectionCount 返回线程安全的累计探测次数。
func (e *countingRuntimeCatalog) detectionCount() int {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return len(e.detections)
}

func (e *countingRuntimeCatalog) lastRequest(runtimeID string) agentapi.DetectRequest {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	for index := len(e.requests) - 1; index >= 0; index-- {
		if e.requests[index].RuntimeID == runtimeID {
			return e.requests[index]
		}
	}
	return agentapi.DetectRequest{}
}

// failCodexOptions 让后续 Codex 探测返回模型目录刷新警告。
func (e *countingRuntimeCatalog) failCodexOptions() {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.codexOptionsUnavailable = true
}

// setPermissionLevels 设置后续探测快照写入模型的产品权限。
func (e *countingRuntimeCatalog) setPermissionLevels(levels []agentapi.PermissionLevel) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.permissionLevels = append([]agentapi.PermissionLevel(nil), levels...)
}

func TestInitializationRegistersWithoutProbingAndRefreshReadsSnapshots(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "runtime-environments.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get test sql database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	environments := controllers.NewRuntimeEnvironmentController(database.DB)
	catalog := &countingRuntimeCatalog{}
	serviceValue, err := newWithRuntimeCatalog(
		catalog,
		environments,
		controllers.NewTransactionController(
			dbhelper.NewGormTransactionManager(database.DB),
		),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	serviceValue.(*service).probeProvider = catalog.probe

	initialized, err := serviceValue.EnsureInitialized(context.Background())
	if err != nil {
		t.Fatalf("first EnsureInitialized() error = %v", err)
	}
	if !initialized || catalog.detectionCount() != 0 {
		t.Fatalf("initialization started an external probe: initialized=%v count=%d", initialized, catalog.detectionCount())
	}
	if err := serviceValue.RefreshAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	first, err := environments.List(context.Background())
	if err != nil {
		t.Fatalf("first repository List() error = %v", err)
	}
	if !initialized || len(first) != 2 || catalog.detectionCount() != 2 {
		t.Fatalf(
			"first initialization = %v entries = %d detections = %d, want true, 2 and 2",
			initialized,
			len(first),
			catalog.detectionCount(),
		)
	}
	for _, environment := range first {
		if environment.DefaultPermissionLevel != agentapi.PermissionLevelFullAccess {
			t.Fatalf(
				"%s default permission = %q, want full-access",
				environment.RuntimeID,
				environment.DefaultPermissionLevel,
			)
		}
	}
	initialized, err = serviceValue.EnsureInitialized(context.Background())
	if err != nil {
		t.Fatalf("second EnsureInitialized() error = %v", err)
	}
	second, err := environments.List(context.Background())
	if err != nil {
		t.Fatalf("second repository List() error = %v", err)
	}
	if initialized || len(second) != 2 || catalog.detectionCount() != 2 {
		t.Fatalf(
			"second initialization = %v entries = %d detections = %d, want false, cached 2 and 2",
			initialized,
			len(second),
			catalog.detectionCount(),
		)
	}
	selected, err := environments.Get(
		context.Background(),
		agentapi.RuntimeIDCodex,
	)
	if err != nil {
		t.Fatalf("repository Get() error = %v", err)
	}
	if selected.Detection.ResolvedBinary != "/usr/local/bin/codex" {
		t.Fatalf(
			"ResolvedBinary = %q, want cached path",
			selected.Detection.ResolvedBinary,
		)
	}
	var claude models.RuntimeEnvironment
	for _, environment := range first {
		if environment.RuntimeID == agentapi.RuntimeIDClaude {
			claude = environment
			break
		}
	}
	claude.ConfigHome = "/tmp/claude-config"
	if err := environments.UpdateConfiguration(context.Background(), claude); err != nil {
		t.Fatalf("UpdateConfiguration() error = %v", err)
	}
	if err := serviceValue.RefreshOne(context.Background(), agentapi.RuntimeIDClaude); err != nil {
		t.Fatalf("RefreshOne() error = %v", err)
	}
	request := catalog.lastRequest(agentapi.RuntimeIDClaude)
	if request.LaunchOverride.Environment["CLAUDE_CONFIG_DIR"] != claude.ConfigHome {
		t.Fatalf(
			"Claude config environment = %#v, want CLAUDE_CONFIG_DIR=%q",
			request.LaunchOverride.Environment,
			claude.ConfigHome,
		)
	}
	codex, err := environments.Get(context.Background(), agentapi.RuntimeIDCodex)
	if err != nil {
		t.Fatalf("get Codex environment: %v", err)
	}
	codex.DefaultModel = "stale-model"
	codex.DefaultReasoning = "high"
	if err := environments.UpdateConfiguration(context.Background(), codex); err != nil {
		t.Fatalf("save stale Codex defaults: %v", err)
	}
	if err := serviceValue.RefreshOne(context.Background(), agentapi.RuntimeIDCodex); err != nil {
		t.Fatalf("refresh Codex defaults: %v", err)
	}
	codex, err = environments.Get(context.Background(), agentapi.RuntimeIDCodex)
	if err != nil {
		t.Fatalf("read normalized Codex environment: %v", err)
	}
	if codex.DefaultModel != "stale-model" || codex.DefaultReasoning != "high" {
		t.Fatalf(
			"Codex defaults = %q/%q, want saved stale-model/high",
			codex.DefaultModel,
			codex.DefaultReasoning,
		)
	}
	catalog.failCodexOptions()
	if err := serviceValue.RefreshOne(context.Background(), agentapi.RuntimeIDCodex); err != nil {
		t.Fatalf("refresh Codex with unavailable options: %v", err)
	}
	codex, err = environments.Get(context.Background(), agentapi.RuntimeIDCodex)
	if err != nil {
		t.Fatalf("read fallback Codex environment: %v", err)
	}
	if len(codex.Detection.Models) != 1 || codex.Detection.Models[0].ID != "gpt-stable" {
		t.Fatalf(
			"Codex models after options failure = %#v, want previous dynamic catalog",
			codex.Detection.Models,
		)
	}
	if err := serviceValue.RefreshOne(context.Background(), agentapi.RuntimeIDCodex); err != nil {
		t.Fatalf("repeat Codex refresh with unavailable options: %v", err)
	}
	codex, err = environments.Get(context.Background(), agentapi.RuntimeIDCodex)
	if err != nil {
		t.Fatalf("read repeated fallback Codex environment: %v", err)
	}
	if len(codex.Detection.Models) != 1 || codex.Detection.Models[0].ID != "gpt-stable" {
		t.Fatalf(
			"Codex models after repeated options failure = %#v, want previous dynamic catalog",
			codex.Detection.Models,
		)
	}
}

func TestRefreshPreservesConfiguredPermissionWhenCatalogChanges(t *testing.T) {
	catalog, environments, serviceValue := newRuntimeEnvironmentTestService(t)
	if _, err := serviceValue.EnsureInitialized(context.Background()); err != nil {
		t.Fatalf("EnsureInitialized() error = %v", err)
	}
	if err := serviceValue.RefreshAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	catalog.setPermissionLevels([]agentapi.PermissionLevel{
		agentapi.PermissionLevelDefault,
		agentapi.PermissionLevelAuto,
	})
	if err := serviceValue.RefreshAll(context.Background()); err != nil {
		t.Fatalf("RefreshAll() error = %v", err)
	}
	listed, err := environments.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) == 0 {
		t.Fatal("expected persisted runtime environments")
	}
	for _, environment := range listed {
		if environment.DefaultPermissionLevel != agentapi.PermissionLevelFullAccess {
			t.Fatalf(
				"%s default permission = %q, want saved full-access",
				environment.RuntimeID,
				environment.DefaultPermissionLevel,
			)
		}
		if hasPermissionDefaultDowngraded(environment.Detection.Diagnostics) {
			t.Fatalf(
				"%s diagnostics = %#v, did not want a permission downgrade diagnostic",
				environment.RuntimeID,
				environment.Detection.Diagnostics,
			)
		}
	}
}

func TestRefreshDoesNotInventPermissionLevelsWhenCatalogOmitsThem(t *testing.T) {
	_, environments, serviceValue := newRuntimeEnvironmentTestService(t)
	if _, err := serviceValue.EnsureInitialized(context.Background()); err != nil {
		t.Fatalf("EnsureInitialized() error = %v", err)
	}
	if err := serviceValue.RefreshAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := serviceValue.RefreshAll(context.Background()); err != nil {
		t.Fatalf("RefreshAll() error = %v", err)
	}
	listed, err := environments.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	for _, environment := range listed {
		if environment.DefaultPermissionLevel != agentapi.PermissionLevelFullAccess {
			t.Fatalf(
				"%s default permission = %q, want unchanged full-access",
				environment.RuntimeID,
				environment.DefaultPermissionLevel,
			)
		}
		if hasPermissionDefaultDowngraded(environment.Detection.Diagnostics) {
			t.Fatalf(
				"%s diagnostics = %#v, did not want invented permission downgrade",
				environment.RuntimeID,
				environment.Detection.Diagnostics,
			)
		}
		for _, model := range environment.Detection.Models {
			if len(model.PermissionLevels) != 0 {
				t.Fatalf(
					"%s model %s permission levels = %#v, want none",
					environment.RuntimeID,
					model.ID,
					model.PermissionLevels,
				)
			}
		}
	}
}

func newRuntimeEnvironmentTestService(t *testing.T) (
	*countingRuntimeCatalog,
	controllers.RuntimeEnvironmentController,
	Service,
) {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "runtime-environments.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get test sql database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	environments := controllers.NewRuntimeEnvironmentController(database.DB)
	catalog := &countingRuntimeCatalog{}
	serviceValue, err := newWithRuntimeCatalog(
		catalog,
		environments,
		controllers.NewTransactionController(
			dbhelper.NewGormTransactionManager(database.DB),
		),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	serviceValue.(*service).probeProvider = catalog.probe
	return catalog, environments, serviceValue
}

func hasPermissionDefaultDowngraded(diagnostics []agentapi.Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "PERMISSION_DEFAULT_DOWNGRADED" {
			return true
		}
	}
	return false
}

// controlledRuntimeCatalog 为刷新回归测试注入版本、路径和探测屏障。
type controlledRuntimeCatalog struct {
	// countingRuntimeCatalog 提供默认目录与探测记录。
	*countingRuntimeCatalog
	// version 是本次 ACP 返回的版本。
	version string
	// binary 覆盖本次解析出的可执行路径。
	binary string
	// executableMissing 模拟当前选中可执行路径临时无法解析。
	executableMissing bool
	// started 通知测试已进入指定智能体探测。
	started chan string
	// blockedRuntime 是等待释放信号的智能体；空值表示全部等待。
	blockedRuntime string
	// release 允许被阻塞的探测继续完成。
	release chan struct{}
}

// Detect 返回受控快照，并按测试屏障等待或报告可执行路径失效。
func (c *controlledRuntimeCatalog) probe(ctx context.Context, summary agentapi.RuntimeSummary, environment models.RuntimeEnvironment, request agentapi.DetectRequest) agentapi.DetectionResult {
	if c.started != nil {
		c.started <- request.RuntimeID
	}
	if c.release != nil && (c.blockedRuntime == "" || c.blockedRuntime == request.RuntimeID) {
		select {
		case <-c.release:
		case <-ctx.Done():
		}
	}
	result := c.countingRuntimeCatalog.probe(ctx, summary, environment, request)
	result.Version = c.version
	if c.executableMissing {
		result.ResolvedBinary = ""
		result.State = agentapi.DetectionStateUnavailable
	}
	if c.binary != "" {
		result.ResolvedBinary = c.binary
	}
	return result
}

// TestRefreshPreservesVersionForSameInstallation 验证空值或未知版本不会覆盖同一路径的有效版本。
func TestRefreshPreservesVersionForSameInstallation(t *testing.T) {
	for _, version := range []string{"", "unknown", " UNKNOWN "} {
		t.Run(version, func(t *testing.T) {
			base, repo, api := newRuntimeEnvironmentTestService(t)
			svc := api.(*service)
			catalog := &controlledRuntimeCatalog{countingRuntimeCatalog: base, version: "1.2.3"}
			svc.runtimeCatalog = catalog
			svc.probeProvider = catalog.probe
			ctx := context.Background()
			if err := svc.RefreshAll(ctx); err != nil {
				t.Fatal(err)
			}
			catalog.version = version
			base.failCodexOptions()
			if err := svc.RefreshOne(ctx, agentapi.RuntimeIDCodex); err != nil {
				t.Fatal(err)
			}
			got, err := repo.Get(ctx, agentapi.RuntimeIDCodex)
			if err != nil {
				t.Fatal(err)
			}
			if got.Detection.Version != "1.2.3" {
				t.Fatalf("known version overwritten: %q", got.Detection.Version)
			}
			for _, installation := range got.Detection.Installations {
				if installation.Path == got.Detection.ResolvedBinary && installation.Version != "1.2.3" {
					t.Fatalf("installation version overwritten: %+v", installation)
				}
			}
			catalog.binary = "/other/bin/codex"
			if err := svc.RefreshOne(ctx, agentapi.RuntimeIDCodex); err != nil {
				t.Fatal(err)
			}
			got, err = repo.Get(ctx, agentapi.RuntimeIDCodex)
			if err != nil {
				t.Fatal(err)
			}
			if got.Detection.Version != "" {
				t.Fatalf("different installation inherited old version: %q", got.Detection.Version)
			}
		})
	}
}

// TestRefreshFillsVersionFromInstallation 验证安装列表的有效版本能补回空 ACP 主版本。
func TestRefreshFillsVersionFromInstallation(t *testing.T) {
	base, repo, api := newRuntimeEnvironmentTestService(t)
	svc := api.(*service)
	catalog := &controlledRuntimeCatalog{countingRuntimeCatalog: base, version: "1.2.3"}
	svc.runtimeCatalog = catalog
	svc.probeProvider = catalog.probe
	ctx := context.Background()
	if err := svc.RefreshAll(ctx); err != nil {
		t.Fatal(err)
	}
	catalog.version = ""
	if err := svc.RefreshOne(ctx, agentapi.RuntimeIDCodex); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, agentapi.RuntimeIDCodex)
	if err != nil {
		t.Fatal(err)
	}
	if got.Detection.Version != "1.2.3" {
		t.Fatalf("installation version not used: %+v", got.Detection)
	}
}

// TestStartupDetectionDoesNotBlockInitializationAndPublishesEachResult 验证启动与首次列表不等待外部探测，完成项及时保存。
func TestStartupDetectionDoesNotBlockInitializationAndPublishesEachResult(t *testing.T) {
	base, repo, api := newRuntimeEnvironmentTestService(t)
	svc := api.(*service)
	release := make(chan struct{})
	catalog := &controlledRuntimeCatalog{countingRuntimeCatalog: base, version: "1.2.3", started: make(chan string, 2), blockedRuntime: agentapi.RuntimeIDCodex, release: release}
	svc.runtimeCatalog = catalog
	svc.probeProvider = catalog.probe
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	returned := make(chan error, 1)
	go func() { returned <- svc.Start(ctx) }()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("Start waited for detection")
	}
	defer svc.Close()
	defer close(release)
	for i := 0; i < 2; i++ {
		select {
		case <-catalog.started:
		case <-time.After(5 * time.Second):
			t.Fatal("runtime probes did not start concurrently")
		}
	}
	initialized := make(chan error, 1)
	go func() { _, err := svc.EnsureInitialized(ctx); initialized <- err }()
	select {
	case err := <-initialized:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("list initialization waited for detection")
	}
	deadline := time.After(5 * time.Second)
	for {
		claude, err := repo.Get(ctx, agentapi.RuntimeIDClaude)
		if err != nil {
			t.Fatal(err)
		}
		if claude.DetectedAt > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("completed probe waited for slower runtime")
		case <-time.After(10 * time.Millisecond):
		}
	}
	codex, err := repo.Get(ctx, agentapi.RuntimeIDCodex)
	if err != nil {
		t.Fatal(err)
	}
	if codex.DetectedAt != 0 || codex.Detection.State != "" {
		t.Fatalf("unfinished probe claimed a completed state: %+v", codex)
	}
	// 重检已完成的 Claude 不得等待仍被阻塞的 Codex，也不得重新探测其他项。
	refreshed := make(chan error, 1)
	go func() { refreshed <- svc.RefreshOne(ctx, agentapi.RuntimeIDClaude) }()
	select {
	case err := <-refreshed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("single runtime refresh waited for unrelated startup probe")
	}
	if count := base.detectionCount(); count != 2 {
		t.Fatalf("completed detections = %d, want only two Claude probes", count)
	}
	if pending, failed, _ := svc.StartupDetection(); !pending || failed {
		t.Fatalf("single refresh changed aggregate startup state: pending=%v failed=%v", pending, failed)
	}
}

// TestRefreshKeepsKnownVersionWhenSelectedExecutableIsMissing 验证失效安装不会清空版本或伪造就绪状态。
func TestRefreshKeepsKnownVersionWhenSelectedExecutableIsMissing(t *testing.T) {
	base, repo, api := newRuntimeEnvironmentTestService(t)
	svc := api.(*service)
	catalog := &controlledRuntimeCatalog{countingRuntimeCatalog: base, version: "1.2.3"}
	svc.runtimeCatalog = catalog
	svc.probeProvider = catalog.probe
	ctx := context.Background()
	if err := svc.RefreshAll(ctx); err != nil {
		t.Fatal(err)
	}
	catalog.version = ""
	catalog.executableMissing = true
	if err := svc.RefreshOne(ctx, agentapi.RuntimeIDCodex); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, agentapi.RuntimeIDCodex)
	if err != nil {
		t.Fatal(err)
	}
	if got.Detection.Version != "1.2.3" || got.Detection.ResolvedBinary != "" || got.Detection.State != agentapi.DetectionStateUnavailable {
		t.Fatalf("missing executable lost version or retained readiness: %+v", got.Detection)
	}
}

// legacyFallbackCatalog exposes the retired method to catch an accidental fallback.
type legacyFallbackCatalog struct {
	*countingRuntimeCatalog
}

func (c legacyFallbackCatalog) Detect(ctx context.Context, request agentapi.DetectRequest) agentapi.DetectionResult {
	return c.probe(ctx, c.Runtimes()[1], models.RuntimeEnvironment{}, request)
}

// TestDetectionWithoutProviderPreservesInstallation verifies device discovery stays available when native probing cannot run.
func TestDetectionWithoutProviderPreservesInstallation(t *testing.T) {
	catalog := legacyFallbackCatalog{&countingRuntimeCatalog{}}
	svc := &service{runtimeCatalog: catalog}
	summary := catalog.Runtimes()[1]
	result := svc.detect(t.Context(), summary, models.RuntimeEnvironment{RuntimeID: summary.ID, BaseRuntimeID: summary.BaseRuntimeID})
	if result.ResolvedBinary == "" || result.State != agentapi.DetectionStateUnavailable || len(result.Installations) == 0 {
		t.Fatalf("detection without provider lost the installed CLI or reported it ready: %+v", result)
	}
	if catalog.detectionCount() != 0 {
		t.Fatal("detection fell back to the legacy catalog")
	}
}

// TestRefreshPreservesSelectedRuntimeAcrossProbeFailure 验证失败快照不会替换默认智能体或改写用户配置。
func TestRefreshPreservesSelectedRuntimeAcrossProbeFailure(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "已启用", false: "已禁用"}[enabled], func(t *testing.T) {
			base, repo, api := newRuntimeEnvironmentTestService(t)
			svc := api.(*service)
			ctx := context.Background()
			if err := svc.RefreshAll(ctx); err != nil {
				t.Fatal(err)
			}
			if err := repo.ClearDefault(ctx); err != nil {
				t.Fatal(err)
			}
			if err := repo.UpdateFields(ctx, agentapi.RuntimeIDCodex, map[string]any{
				"is_default": true, "enabled": enabled, "default_model": "saved-model", "default_reasoning": "saved-thinking", "default_permission_level": agentapi.PermissionLevelFullAccess,
			}); err != nil {
				t.Fatal(err)
			}
			catalog := &controlledRuntimeCatalog{countingRuntimeCatalog: base, executableMissing: true}
			svc.runtimeCatalog, svc.probeProvider = catalog, catalog.probe
			if err := svc.RefreshOne(ctx, agentapi.RuntimeIDCodex); err != nil {
				t.Fatal(err)
			}
			selected, err := repo.Get(ctx, agentapi.RuntimeIDCodex)
			if err != nil {
				t.Fatal(err)
			}
			if !selected.IsDefault || selected.Enabled != enabled || selected.DefaultModel != "saved-model" || selected.DefaultReasoning != "saved-thinking" || selected.DefaultPermissionLevel != agentapi.PermissionLevelFullAccess {
				t.Fatalf("probe changed saved selection: %+v", selected)
			}
			if selected.Detection.State != agentapi.DetectionStateUnavailable {
				t.Fatalf("probe diagnostic was lost: %+v", selected.Detection)
			}
		})
	}
}

// TestStartupKeepsSavedSelectionWithoutProbeEvidence 验证旧失败快照不使已有默认选择重新进入引导。
func TestStartupKeepsSavedSelectionWithoutProbeEvidence(t *testing.T) {
	base, repo, api := newRuntimeEnvironmentTestService(t)
	svc := api.(*service)
	ctx := context.Background()
	if _, err := svc.EnsureInitialized(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateFields(ctx, agentapi.RuntimeIDCodex, map[string]any{"is_default": true}); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	catalog := &controlledRuntimeCatalog{countingRuntimeCatalog: base, release: release}
	svc.runtimeCatalog, svc.probeProvider = catalog, catalog.probe
	if err := svc.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	defer close(release)
	if _, _, required := svc.StartupDetection(); required {
		t.Fatal("saved default was blocked by missing probe evidence")
	}
}
