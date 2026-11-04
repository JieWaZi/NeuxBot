package agent

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestProviderRuntimeVerifiesCodexSystemCredentialBeforePublishing(t *testing.T) {
	for _, tc := range []struct {
		// name names the startup fixture.
		name string
		// account supplies the native account kind.
		account string
		// credentialKind selects the lease authentication kind.
		credentialKind string
		// accessToken supplies the native process proof.
		accessToken string
		// wrongRoot introduces a conflict before starting the native process.
		wrongRoot bool
		// wrongReportedRoot overrides the actual root reported by the native process.
		wrongReportedRoot bool
		// wantErr is the public startup failure.
		wantErr error
	}{
		{name: "matching api key", credentialKind: "api_key", account: "apiKey", accessToken: "fixture-selected"},
		{name: "api key wrong mode", credentialKind: "api_key", account: "chatgpt", accessToken: "fixture-selected", wantErr: status_error.RuntimeAuthRequired},
		{name: "matching", account: "chatgpt", accessToken: "fixture-selected"},
		{name: "wrong native credential", account: "chatgpt", accessToken: "fixture-other", wantErr: status_error.RuntimeAuthRequired},
		{name: "wrong native login kind", account: "apiKey", accessToken: "fixture-selected", wantErr: status_error.RuntimeAuthRequired},
		{name: "conflicting native root", account: "chatgpt", accessToken: "fixture-selected", wrongRoot: true, wantErr: status_error.AIAccountConfigurationInvalid},
		{name: "wrong reported native root", account: "chatgpt", accessToken: "fixture-selected", wrongReportedRoot: true, wantErr: status_error.AIAccountConfigurationInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := providerTestRequest(t, "codex")
			req.Account.AccountID = SystemAccountID
			req.Account.ConfigHome = req.Account.CWD
			req.Launch.Environment["FIXTURE_CODEX_ACCOUNT"] = tc.account
			req.Launch.Environment["FIXTURE_CODEX_TOKEN"] = tc.accessToken
			if tc.wrongRoot {
				req.Launch.ConfigHome = t.TempDir()
			}
			if tc.wrongReportedRoot {
				req.Launch.Environment["FIXTURE_CODEX_ROOT"] = t.TempDir()
			}
			releases := &providerReleaseTrace{}
			runtime := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: func(context.Context, AccountRequest) (AccountLease, error) {
				return AccountLease{
					CodexCredentialKind: tc.credentialKind, ConfigHome: req.Account.ConfigHome, Close: releases.add("account"),
					VerifyCodexCredential: func(_ context.Context, proof [sha256.Size]byte) error {
						if proof != sha256.Sum256([]byte("fixture-selected")) {
							return status_error.RuntimeAuthRequired
						}
						return nil
					},
				}, nil
			}})
			t.Cleanup(func() { _ = runtime.Close(context.Background()) })
			conn, err := runtime.Open(t.Context(), req, ProviderConnectionPorts{})
			if conn == nil || !errors.Is(err, tc.wantErr) {
				t.Fatalf("Open = %v, %v", conn != nil, err)
			}
			if _, err := conn.Client(); (err == nil) != (tc.wantErr == nil) {
				t.Fatalf("publication = %v", err)
			}
			if err := conn.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			releases.require(t, "account")
		})
	}
}

func TestProviderRuntimeNativeInitialization(t *testing.T) {
	for _, provider := range []string{"codex", "claude", "cursor"} {
		t.Run(provider, func(t *testing.T) {
			req := providerTestRequest(t, provider)
			trace := filepath.Join(req.Account.CWD, "trace")
			releases := &providerReleaseTrace{}
			runtime := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: func(_ context.Context, account AccountRequest) (AccountLease, error) {
				if account != req.Account {
					return AccountLease{}, status_error.AIAccountConfigurationInvalid
				}
				return AccountLease{
					Environment: map[string]string{"FIXTURE_ACCOUNT": "chosen"},
					PrepareLaunchAuth: func(context.Context) (LaunchAuthLease, error) {
						return LaunchAuthLease{Environment: map[string]string{"FIXTURE_AUTH": "launch"}, Close: releases.add("auth")}, nil
					},
					Close: releases.add("account"),
				}, nil
			}})
			connection, err := runtime.Open(t.Context(), req, ProviderConnectionPorts{AcquireResources: func(context.Context, ResourceSelection) (ResourceLease, error) {
				return ResourceLease{
					Close: releases.add("resources"),
					CloseFiles: func(context.Context) error {
						releases.add("files")(context.Background())
						return os.Remove(trace)
					},
				}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtime.Close(context.Background()) })
			if _, err := connection.Client(); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(trace)
			if err != nil || !strings.Contains(string(got), req.Account.CWD+"|chosen|launch") {
				t.Fatalf("child launch evidence = %q, %v", got, err)
			}
			if provider != "claude" && !strings.Contains(string(got), "initialize") {
				t.Fatalf("missing native handshake in %q", got)
			}
			if err := connection.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := runtime.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			releases.require(t, "auth", "resources", "account", "files")
			if _, err := os.Stat(trace); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("temporary file survived: %v", err)
			}
			if _, err := connection.Client(); !errors.Is(err, status_error.RuntimeUnavailable) {
				t.Fatalf("closed connection returned a client: %v", err)
			}
		})
	}
}

func TestProviderRuntimePartialAccountRetainsFailedCleanup(t *testing.T) {
	req := providerTestRequest(t, "codex")
	entered := make(chan struct{})
	retry := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-retry:
		default:
			close(retry)
		}
	})
	releases := &providerReleaseTrace{}
	runtime := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: func(context.Context, AccountRequest) (AccountLease, error) {
		first := true
		return AccountLease{Close: func(ctx context.Context) error {
			if first {
				first = false
				releases.add("account failed")(ctx)
				close(entered)
				<-retry
				return errors.New("secret cleanup detail")
			}
			return releases.add("account")(ctx)
		}}, fmt.Errorf("private detail: %w", status_error.RuntimeAuthRequired)
	}})
	conn, err := runtime.Open(t.Context(), req, ProviderConnectionPorts{})
	if conn == nil || err != status_error.RuntimeAuthRequired {
		t.Fatalf("partial acquisition = %v, %v", conn != nil, err)
	}
	<-entered
	conn.mu.Lock()
	attempt := conn.closingAttempt
	conn.mu.Unlock()
	close(retry)
	<-attempt.done
	if err := conn.WaitOpenCleanup(t.Context()); err != status_error.RuntimeShutdownFailed {
		t.Fatalf("failed startup cleanup = %v", err)
	}
	runtime.mu.Lock()
	registered := runtime.connections[conn.id] == conn
	runtime.mu.Unlock()
	if !registered {
		t.Fatal("cleanup failure dropped its owner")
	}
	if err := conn.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	releases.require(t, "account failed", "account")
	if err := conn.WaitOpenCleanup(t.Context()); err != status_error.RuntimeShutdownFailed {
		t.Fatalf("retry replaced the original startup cleanup receipt: %v", err)
	}
}

func TestProviderRuntimeRootCloseRetainsLateResources(t *testing.T) {
	req := providerTestRequest(t, "codex")
	acquiring := make(chan struct{})
	returnResource := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-returnResource:
		default:
			close(returnResource)
		}
	})
	releases := &providerReleaseTrace{}
	runtime := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: func(context.Context, AccountRequest) (AccountLease, error) {
		return AccountLease{Close: releases.add("account")}, nil
	}})
	openCtx, cancelOpen := context.WithCancel(t.Context())
	defer cancelOpen()
	type openResult struct {
		conn *providerConnection
		err  error
	}
	opened := make(chan openResult, 1)
	go func() {
		conn, err := runtime.Open(openCtx, req, ProviderConnectionPorts{AcquireResources: func(context.Context, ResourceSelection) (ResourceLease, error) {
			close(acquiring)
			<-returnResource
			return ResourceLease{Close: releases.add("resource"), CloseFiles: releases.add("files")}, nil
		}})
		opened <- openResult{conn, err}
	}()
	<-acquiring
	other := providerTestRequest(t, "claude")
	other.Account.AccountID = "independent"
	independent, err := runtime.Open(t.Context(), other, ProviderConnectionPorts{})
	if err != nil {
		t.Fatal(err)
	}
	if err := independent.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	closeCtx, cancelClose := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancelClose()
	if err := runtime.Close(closeCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("pending root close = %v", err)
	}
	if _, err := runtime.Open(t.Context(), other, ProviderConnectionPorts{}); err != status_error.RuntimeUnavailable {
		t.Fatalf("root admitted a successor = %v", err)
	}
	releases.require(t, "account")
	cancelOpen()
	original := <-opened
	if original.conn == nil || original.err == nil {
		t.Fatalf("late startup exposed a client: %v, %v", original.conn != nil, original.err)
	}
	close(returnResource)
	if err := runtime.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	releases.require(t, "account", "resource", "account", "files")
	if _, err := os.Stat(filepath.Join(req.Account.CWD, "trace")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("late acquisition started the native process: %v", err)
	}
}

func TestProviderRuntimeInvalidationRetainsProgress(t *testing.T) {
	req := providerTestRequest(t, "codex")
	releases := &providerReleaseTrace{}
	runtime := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: func(context.Context, AccountRequest) (AccountLease, error) {
		return AccountLease{
			PrepareLaunchAuth: func(context.Context) (LaunchAuthLease, error) {
				return LaunchAuthLease{Close: releases.add("auth")}, nil
			},
			Close: releases.add("account"),
		}, nil
	}})
	first := true
	conn, err := runtime.Open(t.Context(), req, ProviderConnectionPorts{AcquireResources: func(context.Context, ResourceSelection) (ResourceLease, error) {
		return ResourceLease{
			Close: func(ctx context.Context) error {
				if first {
					first = false
					releases.add("resources failed")(ctx)
					return errors.New("resource failure")
				}
				return releases.add("resources")(ctx)
			},
			CloseFiles: releases.add("files"),
		}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	account := req.Account
	if err := runtime.InvalidateAccount(t.Context(), account.Provider, account.AccountID, account.AuthGeneration); err != status_error.RuntimeShutdownFailed {
		t.Fatalf("initial invalidation = %v", err)
	}
	releases.require(t, "auth", "resources failed")
	next := req
	next.Account.AuthGeneration++
	if _, err := runtime.Open(t.Context(), next, ProviderConnectionPorts{}); err != status_error.RuntimeShutdownFailed {
		t.Fatalf("new generation bypassed failed owner = %v", err)
	}
	if err := runtime.InvalidateAccount(t.Context(), account.Provider, account.AccountID, account.AuthGeneration); err != nil {
		t.Fatal(err)
	}
	releases.require(t, "auth", "resources failed", "resources", "account", "files")
	if _, err := runtime.Open(t.Context(), req, ProviderConnectionPorts{}); err != status_error.RuntimeUnavailable {
		t.Fatalf("invalidated generation admitted = %v", err)
	}
	successor, err := runtime.Open(t.Context(), next, ProviderConnectionPorts{})
	if err != nil || successor.id == conn.id {
		t.Fatalf("successor = %v, %v", successor != nil, err)
	}
}

func TestProviderRuntimeInitializeFailureSanitizesAndReleases(t *testing.T) {
	req := providerTestRequest(t, "codex")
	req.Launch.Environment["FIXTURE_REJECT"] = "1"
	releases := &providerReleaseTrace{}
	runtime := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: func(context.Context, AccountRequest) (AccountLease, error) {
		return AccountLease{Close: releases.add("account")}, nil
	}})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	conn, err := runtime.Open(t.Context(), req, ProviderConnectionPorts{})
	if conn == nil || err == nil || strings.Contains(err.Error(), "secret rejection") {
		t.Fatalf("failed initialize = %v, %v", conn != nil, err)
	}
	if err := conn.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	releases.require(t, "account")
}

func TestProviderRuntimeCloseDuringInitializeReapsConsumer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-exit assertion requires POSIX signal zero")
	}
	req := providerTestRequest(t, "codex")
	req.Launch.Environment["FIXTURE_HOLD_INITIALIZE"] = "1"
	pidPath := filepath.Join(req.Account.CWD, "initializing.pid")
	runtime := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: func(context.Context, AccountRequest) (AccountLease, error) {
		return AccountLease{Close: func(context.Context) error {
			body, err := os.ReadFile(pidPath)
			if err != nil {
				return err
			}
			pid, err := strconv.Atoi(string(body))
			if err != nil {
				return err
			}
			process, err := os.FindProcess(pid)
			if err != nil {
				return err
			}
			defer process.Release()
			if err := process.Signal(syscall.Signal(0)); err == nil {
				t.Error("account release ran while the native consumer was alive")
				return errors.New("native consumer still alive")
			}
			return nil
		}}, nil
	}})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	type startup struct {
		// conn is the registered owner returned by Open.
		conn *providerConnection
		// err reports startup completion or cancellation.
		err error
	}
	open := make(chan startup, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go func() {
		conn, err := runtime.Open(ctx, req, ProviderConnectionPorts{})
		open <- startup{conn: conn, err: err}
	}()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(pidPath); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("native consumer did not enter initialization")
		case <-tick.C:
		}
	}
	if err := runtime.Close(ctx); err != nil {
		t.Fatal(err)
	}
	result := <-open
	if result.conn == nil || result.err == nil {
		t.Fatalf("shutdown during initialize published startup: %v, %v", result.conn != nil, result.err)
	}
	if _, err := result.conn.Client(); err != status_error.RuntimeUnavailable {
		t.Fatalf("shutdown during initialize left a usable client: %v", err)
	}
}

func TestProviderRuntimeConstructionFailureReleasesObtainedLeases(t *testing.T) {
	req := providerTestRequest(t, "claude")
	req.Launch.Executable = filepath.Join(req.Account.CWD, "missing-executable")
	releases := &providerReleaseTrace{}
	runtime := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: func(context.Context, AccountRequest) (AccountLease, error) {
		return AccountLease{Close: releases.add("account")}, nil
	}})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	conn, err := runtime.Open(t.Context(), req, ProviderConnectionPorts{AcquireResources: func(context.Context, ResourceSelection) (ResourceLease, error) {
		return ResourceLease{Close: releases.add("resource"), CloseFiles: releases.add("files")}, nil
	}})
	if conn == nil || err != status_error.RuntimeConfigurationInvalid {
		t.Fatalf("construction failure = %v, %v", conn != nil, err)
	}
	if err := conn.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	releases.require(t, "resource", "account", "files")
}

func TestProviderRuntimeAdoptedFilesSurviveUntilAccountRelease(t *testing.T) {
	req := providerTestRequest(t, "claude")
	file := filepath.Join(req.Account.CWD, "owned-workspace-marker")
	if err := os.WriteFile(file, []byte("temporary"), 0600); err != nil {
		t.Fatal(err)
	}
	runtime := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: func(context.Context, AccountRequest) (AccountLease, error) {
		return AccountLease{Close: func(context.Context) error {
			_, err := os.Stat(file)
			return err
		}}, status_error.RuntimeAuthRequired
	}})
	conn, err := runtime.Open(t.Context(), req, ProviderConnectionPorts{CloseFiles: func(context.Context) error {
		return os.Remove(file)
	}})
	if conn == nil || err != status_error.RuntimeAuthRequired {
		t.Fatalf("acquisition = %v, %v", conn != nil, err)
	}
	if err := conn.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("adopted files survived failed account acquisition: %v", err)
	}
}

func TestProviderRuntimeCursorQuotaCleanupRetainsOwner(t *testing.T) {
	request := providerTestRequest(t, "cursor")
	entered, cancelled, finish := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	releases := &providerReleaseTrace{}
	runtime := NewProviderRuntime(ProviderRuntimeOptions{
		CleanupTimeout: 150 * time.Millisecond,
		AcquireAccount: func(context.Context, AccountRequest) (AccountLease, error) {
			return AccountLease{
				CursorQuota: &CursorQuotaLease{Endpoint: "http://127.0.0.1:1", ReadAccessToken: func(ctx context.Context) (string, error) {
					close(entered)
					<-ctx.Done()
					close(cancelled)
					<-finish
					return "", ctx.Err()
				}},
				Close: releases.add("account"),
			}, nil
		},
	})
	t.Cleanup(func() {
		unblock.Do(func() { close(finish) })
		_ = runtime.Close(context.Background())
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, err := runtime.Open(ctx, request, ProviderConnectionPorts{CloseFiles: releases.add("files")})
	if err != nil {
		t.Fatal(err)
	}
	client, err := conn.Client()
	if err != nil {
		t.Fatal(err)
	}
	read := make(chan error, 1)
	go func() {
		_, err := client.ReadAccountUsage(ctx)
		read <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("credential consumer did not start")
	}

	account := request.Account
	if err := runtime.InvalidateAccount(ctx, account.Provider, account.AccountID, account.AuthGeneration); err != status_error.RuntimeShutdownFailed {
		t.Fatalf("invalidation while credential is in use = %v", err)
	}
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("invalidation did not signal cancellation")
	}
	releases.require(t)
	next := request
	next.Account.AuthGeneration++
	if _, err := runtime.Open(ctx, next, ProviderConnectionPorts{}); err != status_error.RuntimeShutdownFailed {
		t.Fatalf("new credential generation bypassed retained owner: %v", err)
	}
	unblock.Do(func() { close(finish) })
	if err := conn.Close(ctx); err != nil {
		t.Fatal(err)
	}
	releases.require(t, "account", "files")
	select {
	case err := <-read:
		if err != status_error.RuntimeUnavailable {
			t.Fatalf("interrupted quota consumer = %v", err)
		}
	case <-ctx.Done():
		t.Fatal("quota consumer did not return")
	}
}

type providerReleaseTrace struct {
	// mu guards evidence written by the cleanup worker.
	mu sync.Mutex
	// steps records attempted releases in order.
	steps []string
}

func (r *providerReleaseTrace) add(step string) func(context.Context) error {
	return func(context.Context) error {
		r.mu.Lock()
		r.steps = append(r.steps, step)
		r.mu.Unlock()
		return nil
	}
}

func (r *providerReleaseTrace) require(t *testing.T, want ...string) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if !reflect.DeepEqual(r.steps, want) {
		t.Fatalf("release trace = %v, want %v", r.steps, want)
	}
}

func providerTestRequest(t *testing.T, provider string) ProviderConnectionRequest {
	t.Helper()
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return ProviderConnectionRequest{
		Account: AccountRequest{Provider: provider, AccountID: "chosen", Subject: "credential-subject", AuthGeneration: 1, CWD: cwd},
		Launch: ProviderLaunch{
			Executable: binary, PrefixArgs: []string{"-test.run=^TestProviderRuntimeChild$", "--"},
			Environment: map[string]string{
				"FIXTURE_PROVIDER": provider, "FIXTURE_TRACE": filepath.Join(cwd, "trace"), "GORACE": "atexit_sleep_ms=0",
			},
		},
	}
}

func TestProviderRuntimeChild(t *testing.T) {
	provider := os.Getenv("FIXTURE_PROVIDER")
	if provider == "" {
		return
	}
	trace, err := os.OpenFile(os.Getenv("FIXTURE_TRACE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(2)
	}
	defer trace.Close()
	cwd, _ := os.Getwd()
	fmt.Fprintf(trace, "%s|%s|%s\n", cwd, os.Getenv("FIXTURE_ACCOUNT"), os.Getenv("FIXTURE_AUTH"))
	fmt.Fprintln(trace, strings.Join(os.Args[1:], " "))
	if homeVariable := os.Getenv("FIXTURE_HOME_VARIABLE"); homeVariable != "" {
		fmt.Fprintf(trace, "selected-home=%s\nselected-auth=%s\n", os.Getenv(homeVariable), os.Getenv("FIXTURE_AUTHORITY"))
	}
	if slices.Contains(os.Args, "--version") {
		switch provider {
		case "claude":
			fmt.Println("2.1.128 (Claude Code)")
		case "cursor":
			fmt.Println("2026.10.27-abcdef0")
		}
		os.Exit(0)
	}
	out := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var frame struct {
			ID        json.RawMessage
			Method    string
			Type      string
			RequestID string `json:"request_id"`
			Request   struct {
				Subtype string
			}
		}
		if json.Unmarshal(scanner.Bytes(), &frame) != nil {
			os.Exit(2)
		}
		fmt.Fprintln(trace, string(scanner.Bytes()))
		if provider == "claude" && frame.Type == "control_request" {
			response := map[string]any{}
			if frame.Request.Subtype == "mcp_status" && os.Getenv("FIXTURE_MCP_REPORT") != "" {
				response["mcpServers"] = []map[string]string{{"name": "requested", "status": "connected"}}
			}
			if err := out.Encode(map[string]any{"type": "control_response", "response": map[string]any{
				"subtype": "success", "request_id": frame.RequestID, "response": response,
			}}); err != nil {
				os.Exit(2)
			}
			continue
		}
		if len(frame.ID) == 0 {
			continue
		}
		if frame.Method == "initialize" && os.Getenv("FIXTURE_HOLD_INITIALIZE") == "1" {
			path := filepath.Join(cwd, "initializing.pid")
			temporary := path + ".tmp"
			if os.WriteFile(temporary, []byte(strconv.Itoa(os.Getpid())), 0600) != nil || os.Rename(temporary, path) != nil {
				os.Exit(2)
			}
			continue
		}
		reply := map[string]any{"jsonrpc": "2.0", "id": frame.ID}
		if os.Getenv("FIXTURE_REJECT") == "1" {
			reply["error"] = map[string]any{"code": -32602, "message": "secret rejection"}
		} else if provider == "codex" {
			switch frame.Method {
			case "initialize":
				home := os.Getenv("FIXTURE_CODEX_ROOT")
				if home == "" {
					home = os.Getenv("CODEX_HOME")
				}
				reply["result"] = map[string]any{"userAgent": "codex/0.163.0", "codexHome": home}
			case "account/read":
				result := map[string]any{}
				if kind := os.Getenv("FIXTURE_CODEX_ACCOUNT"); kind != "" {
					result["requiresOpenaiAuth"] = true
					result["account"] = map[string]any{"type": kind, "email": "fixture@example.invalid", "planType": "plus"}
				}
				reply["result"] = result
			case "getAuthStatus":
				method := "chatgpt"
				if os.Getenv("FIXTURE_CODEX_ACCOUNT") == "apiKey" {
					method = "apikey"
				}
				reply["result"] = map[string]any{"authMethod": method, "authToken": os.Getenv("FIXTURE_CODEX_TOKEN"), "requiresOpenaiAuth": true}
			case "thread/fork":
				reply["result"] = map[string]any{"thread": map[string]any{"id": "fixture-child"}, "model": "fixture-model"}
			case "model/list":
				if os.Getenv("FIXTURE_CODEX_MALFORMED_MODELS") != "" {
					reply["result"] = map[string]any{}
					break
				}
				reply["result"] = map[string]any{"data": []any{map[string]any{
					"id": "internal-model-id", "model": "fixture-model", "displayName": "Fixture",
					"isDefault": true, "defaultReasoningEffort": "medium",
					"supportedReasoningEfforts": []any{map[string]any{"reasoningEffort": "medium"}, map[string]any{"reasoningEffort": "high"}},
				}}}
			default:
				reply["result"] = map[string]any{}
			}
		} else {
			switch frame.Method {
			case "initialize":
				reply["result"] = map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{
					"promptCapabilities": map[string]any{"image": true},
				}}
			case "session/new":
				result := map[string]any{"sessionId": "fixture-native"}
				if os.Getenv("FIXTURE_MCP_REPORT") != "" {
					result["_meta"] = map[string]any{"claude.launch": map[string]any{
						"mcp_servers": []map[string]string{{"name": "requested", "status": "connected"}},
					}}
				}
				reply["result"] = result
			case "session/prompt":
				_ = out.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
					"sessionId": "fixture-native",
					"update":    map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "native answer"}},
				}})
				reply["result"] = map[string]any{"stopReason": "end_turn"}
			}
		}
		if err := out.Encode(reply); err != nil {
			os.Exit(2)
		}
	}
	os.Exit(0)
}
