package aiaccount

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	acp "github.com/JieWaZi/acp-go-sdk"
	"github.com/JieWaZi/acp-go/pkg/claude"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

type subscriptionClaudeKeychain struct {
	// mutex 保护测试原生消费者共享的精确项存储。
	mutex sync.Mutex
	// values 按独立配置根保存原生凭据。
	values map[string][]byte
	// readErr 模拟锁定或不可读的已有原生凭据。
	readErr error
	// writes 检查不可读凭据是否被旧投射覆盖。
	writes int
}

func (k *subscriptionClaudeKeychain) Read(_ context.Context, home string) ([]byte, error) {
	k.mutex.Lock()
	defer k.mutex.Unlock()
	if k.readErr != nil {
		return nil, k.readErr
	}
	value, ok := k.values[home]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), value...), nil
}
func (k *subscriptionClaudeKeychain) Write(_ context.Context, home string, data []byte) error {
	k.mutex.Lock()
	defer k.mutex.Unlock()
	k.writes++
	k.values[home] = append([]byte(nil), data...)
	return nil
}
func (k *subscriptionClaudeKeychain) Remove(_ context.Context, home string) error {
	k.mutex.Lock()
	defer k.mutex.Unlock()
	delete(k.values, home)
	return nil
}

type subscriptionClaudeClient struct {
	// home 是传给原生客户端的显式配置根。
	home string
	// snapshot 是 Fake 官方授权所产生的原生文档。
	snapshot accountconfig.ClaudeCredentialSnapshot
	// store 是测试凭据搬运器，不接触真实 Keychain。
	store *accountconfig.ClaudeCredentialStore
	// authenticate 可暂停认证以验证取消与独占边界。
	authenticate func(context.Context) error
	// closeFailures 控制关闭失败后的同所有者重试。
	closeFailures *atomic.Int32
}

func (c *subscriptionClaudeClient) Initialize(context.Context, acp.InitializeRequest) (acp.InitializeResponse, error) {
	return acp.InitializeResponse{}, nil
}
func (c *subscriptionClaudeClient) Authenticate(ctx context.Context, req acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	if req.MethodId != constants.ClaudeSubscriptionAuthMethod {
		return acp.AuthenticateResponse{}, status_error.BadRequest
	}
	if c.authenticate != nil {
		if err := c.authenticate(ctx); err != nil {
			return acp.AuthenticateResponse{}, err
		}
	}
	return acp.AuthenticateResponse{}, c.store.Write(ctx, c.home, c.snapshot)
}
func (c *subscriptionClaudeClient) Logout(ctx context.Context, _ acp.LogoutRequest) (acp.LogoutResponse, error) {
	return acp.LogoutResponse{}, c.store.Remove(ctx, c.home)
}
func (c *subscriptionClaudeClient) ReadLoginStatus(context.Context) (claude.LoginStatus, error) {
	return claude.LoginStatus{Authenticated: true, Kind: "subscription", AccountID: c.snapshot.AccountID, OrganizationID: c.snapshot.OrganizationID, ConfigDirectory: c.home}, nil
}
func (c *subscriptionClaudeClient) Close(context.Context) error {
	if c.closeFailures != nil && c.closeFailures.Add(-1) >= 0 {
		return status_error.RuntimeShutdownFailed
	}
	return nil
}

func claudeSubscriptionFixture(t *testing.T) (*Service, models.AIAccount, *accountconfig.ClaudeCredentialSnapshot) {
	t.Helper()
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(root, "accounts.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	service := New(controllers.NewAIAccountController(database.DB), root)
	service.claudeCredentials = accountconfig.NewClaudeCredentialStore(filepath.Join(root, "ai-accounts", constants.RuntimeIDClaude), &subscriptionClaudeKeychain{values: map[string][]byte{}})
	service.SetProgramResolver(func(context.Context, string) (string, []string, error) { return "/fixture/claude", nil, nil })
	snapshot := &accountconfig.ClaudeCredentialSnapshot{AccountID: "account-a", OrganizationID: "org-a", Credential: json.RawMessage(`{"claudeAiOauth":{"accessToken":"fixture-access","refreshToken":"fixture-refresh","expiresAt":100}}`), Metadata: json.RawMessage(`{"oauthAccount":{"accountUuid":"account-a","organizationUuid":"org-a","emailAddress":"fixture@example.test"}}`)}
	service.claudeFactory = func(_ context.Context, options claude.Options) (claudeSubscriptionClient, error) {
		home := ""
		secure := ""
		for _, entry := range options.Environment {
			if strings.HasPrefix(entry, "CLAUDE_CONFIG_DIR=") {
				home = strings.TrimPrefix(entry, "CLAUDE_CONFIG_DIR=")
			}
			if strings.HasPrefix(entry, "CLAUDE_SECURESTORAGE_CONFIG_DIR=") {
				secure = strings.TrimPrefix(entry, "CLAUDE_SECURESTORAGE_CONFIG_DIR=")
			}
		}
		if home == "" || secure != home || !strings.HasPrefix(home, filepath.Join(root, "ai-accounts", constants.RuntimeIDClaude)+string(filepath.Separator)) {
			return nil, status_error.AIAccountConfigurationInvalid
		}
		return &subscriptionClaudeClient{home: home, snapshot: *snapshot, store: service.claudeCredentials}, nil
	}
	service.Start(t.Context())
	t.Cleanup(service.Close)
	account := models.AIAccount{ID: "0123456789abcdef0123456789abcdef", RuntimeID: constants.RuntimeIDClaude, Name: "Claude subscription", AuthMode: constants.AccountAuthModeSubscription, Endpoint: models.AIAccountEndpoint{Protocol: constants.AccountProtocolAnthropicMessages, BaseURL: constants.AIAccountAnthropicURL}}
	if err := service.accounts.Create(t.Context(), account, ""); err != nil {
		t.Fatal(err)
	}
	account, err = service.accounts.Get(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	return service, account, snapshot
}
func awaitClaudeLogin(t *testing.T, s *Service, accountID, jobID string) Login {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		view, err := s.GetLogin(t.Context(), accountID, jobID)
		if err != nil {
			t.Fatal(err)
		}
		if view.Complete || view.ErrorCode != "" {
			return view
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("Claude login did not finish")
	return Login{}
}
func loginClaudeFixture(t *testing.T, s *Service, account models.AIAccount) models.AIAccount {
	t.Helper()
	view, err := s.StartLogin(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.VerificationURL != "" || view.UserCode != "" {
		t.Fatal("browser authentication fabricated device data")
	}
	view = awaitClaudeLogin(t, s, account.ID, view.ID)
	if !view.Complete || view.ErrorCode != "" {
		t.Fatalf("native login failed: %+v", view)
	}
	current, err := s.accounts.Get(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	return current
}
func TestClaudeSubscriptionLoginLeaseRefreshAndLogout(t *testing.T) {
	service, account, snapshot := claudeSubscriptionFixture(t)
	system := t.TempDir()
	marker := filepath.Join(system, constants.ClaudeCredentialsFileName)
	if err := os.WriteFile(marker, []byte("system-fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", system)
	t.Setenv("CLAUDE_SECURESTORAGE_CONFIG_DIR", system)
	account = loginClaudeFixture(t, service, account)
	if !account.HasCredential || account.AuthGeneration != 2 || strings.Contains(account.CredentialCiphertext, "fixture-access") {
		t.Fatal("native credential was not encrypted with a new generation")
	}
	request, err := service.ResolveAccount(t.Context(), agent.AccountRequest{Provider: constants.RuntimeIDClaude, AccountID: account.ID, Executable: "/fixture/claude", CWD: t.TempDir()}, "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := service.AcquireAccount(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Environment["CLAUDE_CONFIG_DIR"] != request.ConfigHome || lease.Environment["CLAUDE_SECURESTORAGE_CONFIG_DIR"] != request.ConfigHome || request.ConfigHome == system {
		t.Fatal("managed lease selected system storage")
	}
	if release, err := service.Exclusive(account.ID); !errors.Is(err, status_error.AIAccountBusy) {
		if release != nil {
			release()
		}
		t.Fatal("active lease did not block reauthentication", err)
	}
	refreshed := *snapshot
	refreshed.Credential = json.RawMessage(`{"claudeAiOauth":{"accessToken":"fixture-refreshed","refreshToken":"fixture-next","expiresAt":200}}`)
	if err := service.claudeCredentials.Write(t.Context(), request.ConfigHome, refreshed); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	saved, err := service.claudeStoredCredential(t.Context(), account)
	if err != nil || !strings.Contains(string(saved.Credential), "fixture-refreshed") {
		t.Fatal("native refresh was not persisted", err)
	}
	current, err := service.accounts.Get(t.Context(), account.ID)
	if err != nil || current.AuthGeneration != account.AuthGeneration || current.Revision != account.Revision {
		t.Fatal("token refresh changed account configuration or identity")
	}
	release, err := service.Exclusive(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := service.LogoutClaudeSubscription(t.Context(), current); err != nil {
		t.Fatal(err)
	}
	if _, err := service.claudeCredentials.Read(t.Context(), request.ConfigHome); err == nil {
		t.Fatal("native logout retained the selected projection")
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "system-fixture" {
		t.Fatal("managed operations changed system credentials")
	}
}
func TestClaudeSubscriptionReauthenticationCannotReplacePrincipal(t *testing.T) {
	service, account, snapshot := claudeSubscriptionFixture(t)
	account = loginClaudeFixture(t, service, account)
	before, err := service.accounts.Credential(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.AccountID = "different-account"
	view, err := service.StartLogin(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	view = awaitClaudeLogin(t, service, account.ID, view.ID)
	if view.Complete || view.ErrorCode == "" {
		t.Fatal("reauthentication replaced a different native principal")
	}
	after, err := service.accounts.Credential(t.Context(), account.ID)
	if err != nil || before != after {
		t.Fatal("rejected reauthentication changed durable credential")
	}
}
func TestClaudeSubscriptionCancellationPreservesPreviousCredential(t *testing.T) {
	service, account, _ := claudeSubscriptionFixture(t)
	account = loginClaudeFixture(t, service, account)
	before, _ := service.accounts.Credential(t.Context(), account.ID)
	original := service.claudeFactory
	started := make(chan struct{})
	service.claudeFactory = func(ctx context.Context, options claude.Options) (claudeSubscriptionClient, error) {
		client, err := original(ctx, options)
		if err != nil {
			return nil, err
		}
		fake := client.(*subscriptionClaudeClient)
		fake.authenticate = func(ctx context.Context) error { close(started); <-ctx.Done(); return status_error.OperationCanceled }
		return fake, nil
	}
	view, err := service.StartLogin(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	service.CancelLogin(account.ID, view.ID)
	view = awaitClaudeLogin(t, service, account.ID, view.ID)
	if view.Complete || view.ErrorCode == "" {
		t.Fatal("cancelled native authorization committed")
	}
	after, _ := service.accounts.Credential(t.Context(), account.ID)
	if before != after {
		t.Fatal("cancelled authorization replaced prior tokens")
	}
}
func TestClaudeSubscriptionCloseFailureRetainsOwnerUntilRetry(t *testing.T) {
	service, account, _ := claudeSubscriptionFixture(t)
	original := service.claudeFactory
	var failures atomic.Int32
	failures.Store(2)
	service.claudeFactory = func(ctx context.Context, options claude.Options) (claudeSubscriptionClient, error) {
		client, err := original(ctx, options)
		if err == nil {
			client.(*subscriptionClaudeClient).closeFailures = &failures
		}
		return client, err
	}
	view, err := service.StartLogin(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	view = awaitClaudeLogin(t, service, account.ID, view.ID)
	if view.Complete || view.ErrorCode == "" {
		t.Fatal("failed native shutdown committed login")
	}
	if _, err := service.Exclusive(account.ID); !errors.Is(err, status_error.AIAccountBusy) {
		t.Fatal("failed shutdown released native owner")
	}
	service.mutex.Lock()
	job := service.jobs[account.ID]
	service.mutex.Unlock()
	<-job.claudeExit
	service.CancelLogin(account.ID, view.ID)
	release, err := service.Exclusive(account.ID)
	if err != nil {
		t.Fatal("cleanup retry did not release the same owner", err)
	}
	release()
}

func TestClaudeSubscriptionOldGenerationCannotOverwriteNewCredential(t *testing.T) {
	service, account, _ := claudeSubscriptionFixture(t)
	account = loginClaudeFixture(t, service, account)
	request, err := service.ResolveAccount(t.Context(), agent.AccountRequest{Provider: constants.RuntimeIDClaude, AccountID: account.ID, Executable: "/fixture/claude", CWD: t.TempDir()}, "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := service.AcquireAccount(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	newer, err := service.claudeStoredCredential(t.Context(), account)
	if err != nil {
		t.Fatal(err)
	}
	newer.Credential = json.RawMessage(`{"claudeAiOauth":{"accessToken":"new-generation","refreshToken":"new-refresh","expiresAt":300}}`)
	encoded, _ := json.Marshal(newer)
	if err := service.accounts.StoreLoginCredential(t.Context(), account.ID, account.Revision, string(encoded)); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := lease.Close(canceled); err != nil {
		t.Fatal("closed old native consumer did not release its lease", err)
	}
	saved, err := service.accounts.Credential(t.Context(), account.ID)
	if err != nil || saved != string(encoded) {
		t.Fatal("old generation replaced a new login")
	}
}

func TestClaudeSubscriptionUnreadableProjectionIsNeverOverwritten(t *testing.T) {
	service, account, snapshot := claudeSubscriptionFixture(t)
	account = loginClaudeFixture(t, service, account)
	request, err := service.ResolveAccount(t.Context(), agent.AccountRequest{Provider: constants.RuntimeIDClaude, AccountID: account.ID, Executable: "/fixture/claude", CWD: t.TempDir()}, "")
	if err != nil {
		t.Fatal(err)
	}
	home, err := service.managedAccountHome(constants.RuntimeIDClaude, account, true)
	if err != nil {
		t.Fatal(err)
	}
	keychain := &subscriptionClaudeKeychain{values: map[string][]byte{home: []byte("unreadable-fixture")}, readErr: status_error.AIAccountCredentialError}
	if runtime.GOOS == "darwin" {
		service.claudeCredentials = accountconfig.NewClaudeCredentialStore(filepath.Join(service.dataDirectory, "ai-accounts", constants.RuntimeIDClaude), keychain)
	} else {
		if err := os.WriteFile(filepath.Join(home, constants.ClaudeCredentialsFileName), []byte("unreadable-fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.AcquireAccount(t.Context(), request); !errors.Is(err, status_error.AIAccountCredentialError) {
		t.Fatalf("unreadable native storage accepted: %v", err)
	}
	if runtime.GOOS == "darwin" {
		if keychain.writes != 0 || string(keychain.values[home]) != "unreadable-fixture" {
			t.Fatal("locked keychain was overwritten by durable snapshot")
		}
	} else {
		data, _ := os.ReadFile(filepath.Join(home, constants.ClaudeCredentialsFileName))
		if string(data) != "unreadable-fixture" {
			t.Fatal("unreadable file was overwritten")
		}
	}
	if string(snapshot.Credential) == "" {
		t.Fatal("fixture lost original database credential")
	}
}

func TestClaudeSubscriptionConcurrentFirstLeasesShareOneProjection(t *testing.T) {
	service, account, _ := claudeSubscriptionFixture(t)
	account = loginClaudeFixture(t, service, account)
	request, err := service.ResolveAccount(t.Context(), agent.AccountRequest{Provider: constants.RuntimeIDClaude, AccountID: account.ID, Executable: "/fixture/claude", CWD: t.TempDir()}, "")
	if err != nil {
		t.Fatal(err)
	}
	gateRelease, err := service.credentialGate(account.ID).Lock(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var releaseGate sync.Once
	unlock := func() { releaseGate.Do(gateRelease) }
	defer unlock()
	type acquired struct {
		// lease 是独立聊天持有的原生账号租约。
		lease agent.AccountLease
		// err 保存该次准入的实际结果。
		err error
	}
	results := make(chan acquired, 2)
	for range 2 {
		go func() {
			lease, err := service.AcquireAccount(t.Context(), request)
			results <- acquired{lease: lease, err: err}
		}()
	}
	// 两个读租约都已经登记，再允许任何消费者进入首次凭据物化。
	deadline := time.Now().Add(3 * time.Second)
	for {
		service.mutex.Lock()
		readers := service.readers[account.ID]
		service.mutex.Unlock()
		if readers == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("both preparations did not reach the credential gate")
		}
		time.Sleep(time.Millisecond)
	}
	unlock()
	leases := make([]agent.AccountLease, 0, 2)
	for range 2 {
		select {
		case result := <-results:
			if result.err != nil {
				t.Fatalf("concurrent first lease was rejected: %v", result.err)
			}
			if result.lease.ConfigHome != request.ConfigHome {
				t.Fatal("concurrent consumers selected different native generations")
			}
			leases = append(leases, result.lease)
		case <-time.After(3 * time.Second):
			t.Fatal("concurrent preparation did not finish")
		}
	}
	if release, err := service.Exclusive(account.ID); !errors.Is(err, status_error.AIAccountBusy) {
		if release != nil {
			release()
		}
		t.Fatal("concurrent active leases did not block mutation")
	}
	for _, lease := range leases {
		if err := lease.VerifyLogin(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := lease.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	release, err := service.Exclusive(account.ID)
	if err != nil {
		t.Fatal("concurrent leases did not fully release", err)
	}
	release()
}

func claudeFailManagementCloses(service *Service, failures int32) *atomic.Int32 {
	original := service.claudeFactory
	remaining := &atomic.Int32{}
	remaining.Store(failures)
	created := &atomic.Int32{}
	service.claudeFactory = func(ctx context.Context, options claude.Options) (claudeSubscriptionClient, error) {
		created.Add(1)
		client, err := original(ctx, options)
		if err == nil {
			client.(*subscriptionClaudeClient).closeFailures = remaining
		}
		return client, err
	}
	return created
}
func claudeReadyRequest(t *testing.T, service *Service, account models.AIAccount) agent.AccountRequest {
	t.Helper()
	request, err := service.ResolveAccount(t.Context(), agent.AccountRequest{Provider: constants.RuntimeIDClaude, AccountID: account.ID, Executable: "/fixture/claude", CWD: t.TempDir()}, "")
	if err != nil {
		t.Fatal(err)
	}
	return request
}
func assertClaudeRetainedReaders(t *testing.T, service *Service, id string, want int) {
	t.Helper()
	service.mutex.Lock()
	actual := service.readers[id]
	service.mutex.Unlock()
	if actual != want {
		t.Fatalf("native owner released before close confirmation: readers=%d want=%d", actual, want)
	}
}
func TestClaudeSubscriptionLogoutCloseFailureKeepsExclusiveOwner(t *testing.T) {
	service, account, _ := claudeSubscriptionFixture(t)
	account = loginClaudeFixture(t, service, account)
	release, err := service.Exclusive(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	created := claudeFailManagementCloses(service, 2)
	if err := service.LogoutClaudeSubscription(t.Context(), account); !errors.Is(err, status_error.RuntimeShutdownFailed) {
		t.Fatalf("logout close failure not reported: %v", err)
	}
	// 模拟 API 的 defer；原生关闭失败时不能把独占所有权释放给下一次修改。
	release()
	assertClaudeRetainedReaders(t, service, account.ID, -1)
	if _, err := service.Exclusive(account.ID); !errors.Is(err, status_error.RuntimeShutdownFailed) {
		t.Fatalf("logout pending owner did not block mutation: %v", err)
	}
	assertClaudeRetainedReaders(t, service, account.ID, -1)
	nextRelease, err := service.Exclusive(account.ID)
	if err != nil {
		t.Fatal("logout original owner did not recover", err)
	}
	nextRelease()
	if created.Load() != 1 {
		t.Fatal("logout cleanup created another client")
	}
	assertClaudeRetainedReaders(t, service, account.ID, 0)
}

func blockClaudeProjectionRead(t *testing.T, service *Service, home string) func() {
	t.Helper()
	if runtime.GOOS == "darwin" {
		original := service.claudeCredentials
		service.claudeCredentials = accountconfig.NewClaudeCredentialStore(filepath.Join(service.dataDirectory, "ai-accounts", constants.RuntimeIDClaude), &subscriptionClaudeKeychain{readErr: status_error.AIAccountCredentialError})
		return func() { service.claudeCredentials = original }
	}
	path := filepath.Join(home, constants.ClaudeCredentialsFileName)
	backup := path + ".fixture-backup"
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(backup, path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestClaudeSubscriptionLocalReadFailureRetainsLeaseUntilFreshPersistence(t *testing.T) {
	service, account, snapshot := claudeSubscriptionFixture(t)
	account = loginClaudeFixture(t, service, account)
	request := claudeReadyRequest(t, service, account)
	first, err := service.AcquireAccount(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.AcquireAccount(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	restore := blockClaudeProjectionRead(t, service, request.ConfigHome)
	if err := first.VerifyLogin(t.Context()); !errors.Is(err, status_error.AIAccountCredentialError) {
		t.Fatalf("unreadable local credentials were accepted: %v", err)
	}
	if err := first.Close(t.Context()); !errors.Is(err, status_error.AIAccountCredentialError) {
		t.Fatalf("failed local synchronization released the account lease: %v", err)
	}
	assertClaudeRetainedReaders(t, service, account.ID, 2)
	restore()
	latest := *snapshot
	latest.Credential = json.RawMessage(`{"claudeAiOauth":{"accessToken":"other-lease-newest","refreshToken":"other-refresh","expiresAt":800}}`)
	if err := service.claudeCredentials.Write(t.Context(), request.ConfigHome, latest); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, err := service.accounts.Credential(t.Context(), account.ID)
	if err != nil || !strings.Contains(before, "other-lease-newest") {
		t.Fatal("second lease did not persist current credentials", err)
	}
	if err := first.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, err := service.accounts.Credential(t.Context(), account.ID)
	if err != nil || after != before {
		t.Fatal("retry overwrote a newer native refresh", err)
	}
	assertClaudeRetainedReaders(t, service, account.ID, 0)
}
