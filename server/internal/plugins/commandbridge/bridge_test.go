package commandbridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestBridgeMergePathKeepsAllPrivateProgramsBeforeInheritedPath(t *testing.T) {
	first, second, system := t.TempDir(), t.TempDir(), t.TempDir()
	second, err := filepath.EvalSymlinks(second)
	if err != nil {
		t.Fatal(err)
	}
	join := func(paths ...string) string { return strings.Join(paths, string(os.PathListSeparator)) }
	driver := New(Config{DataDirectory: second, Environment: []string{"PATH=" + system}})
	merged, err := driver.MergePath(join(first, system), join(second, system, second))
	if err != nil || !slices.Equal(filepath.SplitList(merged), []string{first, second, system}) {
		t.Fatalf("private commands lost precedence or were duplicated: %q %v", merged, err)
	}
	repeated, err := driver.MergePath(merged, join(second, system))
	if err != nil || repeated != merged {
		t.Fatalf("repeated skill binding changed PATH: %q %v", repeated, err)
	}
	if initial, err := driver.MergePath("", join(second, system)); err != nil || initial != join(second, system) {
		t.Fatalf("first plugin PATH changed: %q %v", initial, err)
	}
}

func TestBridgeMergePathRejectsUntrustedAndEscapingDirectories(t *testing.T) {
	private, external := t.TempDir(), t.TempDir()
	driver := New(Config{DataDirectory: private})
	for _, incoming := range []string{external, filepath.Join(private, "missing"), "."} {
		if _, err := driver.MergePath(private, incoming); !errors.Is(err, status_error.PluginForbidden) {
			t.Fatalf("untrusted incoming path accepted: %q %v", incoming, err)
		}
	}
	escape := filepath.Join(private, "escape")
	if err := os.Symlink(external, escape); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := driver.MergePath(private, escape); !errors.Is(err, status_error.PluginForbidden) {
		t.Fatalf("symlink escaped private identity root: %v", err)
	}
}

func testDriver(t *testing.T, mode string) *Driver {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return New(Config{Command: binary, Args: []string{"-test.run=^TestBridgeProcess$"}, Directory: t.TempDir(), DataDirectory: t.TempDir(), PluginID: "plugin-one", VersionID: "version-one", Digest: strings.Repeat("a", 64), ConfigurationRevision: 3, AccountGeneration: 2, Environment: append(os.Environ(), "NEUXALLY_BRIDGE_TEST="+mode)})
}

func TestBridgeProcess(t *testing.T) {
	mode := os.Getenv("NEUXALLY_BRIDGE_TEST")
	if mode == "" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			// ID 是输入请求序号。
			ID int `json:"id"`
			// Method 是桥接方法。
			Method string `json:"method"`
			// Params 保留宿主范围。
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(2)
		}
		reply := func(value any) {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": value})
		}
		switch request.Method {
		case "auth/initialize":
			protocol := constants.PluginAuthProtocol
			if mode == "wrong-protocol" {
				protocol = "neuxally-auth/1"
			}
			if mode == "wrong-context" {
				request.Params["accountGeneration"] = 99
			}
			reply(map[string]any{"protocol": protocol, "context": request.Params})
		case "auth/status":
			if mode == "dependency-rpc" || mode == "dependency-rpc-exit" {
				_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32000, "message": "dependency_unavailable"}})
				if mode == "dependency-rpc-exit" {
					os.Exit(0)
				}
				continue
			}
			reply(map[string]any{"state": "authorized", "account": map[string]any{"configured": true, "available": true, "id": "public-user", "scopes": []string{"documents:read"}}})
		case "auth/start":
			reply(map[string]any{"state": "pending"})
			if mode == "dependency-state" || mode == "unknown-failure" || mode == "foreign-failure" {
				errorCode := "dependency_unavailable"
				if mode == "unknown-failure" {
					errorCode = "https://example.invalid/?token=private"
				}
				operation := request.Params["operationId"]
				if mode == "foreign-failure" {
					operation = "previous-operation"
				}
				_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "method": "auth/state", "params": map[string]any{"operationId": operation, "sessionId": request.Params["sessionId"], "state": "failed", "errorCode": errorCode}})
				continue
			}
			if mode == "slow-preparation" {
				time.Sleep(31 * time.Second)
			}
			if mode == "preparing" {
				if err := os.WriteFile(filepath.Join(request.Params["dataDirectory"].(string), "preparing"), nil, 0600); err != nil {
					os.Exit(2)
				}
				continue
			}
			session := request.Params["sessionId"]
			if mode == "forged" {
				session = "another-session"
			}
			params := map[string]any{"operationId": request.Params["operationId"], "sessionId": session, "challengeId": "challenge-one", "actions": []any{map[string]any{"type": "copy-code", "value": "ONE"}}, "deadlineAt": time.Now().Add(time.Minute).UnixMilli()}
			if mode == "foreign-operation" {
				params["operationId"] = "previous-operation"
			}
			if mode == "short-challenge" {
				params["deadlineAt"] = time.Now().Add(200 * time.Millisecond).UnixMilli()
			}
			if mode == "qr" {
				params["actions"] = []any{map[string]any{"type": "show-qr", "value": os.Getenv("NEUXALLY_QR_VALUE")}}
			}
			if mode == "notice" {
				encoded, _ := json.Marshal(params)
				// 直接写原始 JSON，确保非法 UTF-8 不会被测试编码器替换。
				fmt.Fprintf(os.Stdout, `{"jsonrpc":"2.0","method":"auth/challenge","params":%s,"notice":%s}}`+"\n", encoded[:len(encoded)-1], os.Getenv("NEUXALLY_NOTICE_JSON"))
			} else {
				_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "method": "auth/challenge", "params": params})
			}
			if mode != "cancel" && mode != "short-challenge" {
				_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "method": "auth/state", "params": map[string]any{"operationId": request.Params["operationId"], "sessionId": session, "state": "authorized"}})
			}
			if mode == "terminal-exit" {
				os.Exit(0)
			}
		case "auth/resolve":
			if strings.HasPrefix(mode, "skill-env") {
				component := request.Params["components"].([]any)[0].(map[string]any)
				key, value := "TOKEN", "secret"
				if mode == "skill-env-private" {
					key = "CACHE"
					value = request.Params["dataDirectory"].(string)
				}
				if mode == "skill-env-escape" {
					key = "CACHE"
					value = os.TempDir()
				}
				if mode == "skill-env-home" {
					key = "HOME"
				}
				reply(map[string]any{"bindings": []any{map[string]any{"componentId": component["componentId"], "environment": map[string]string{key: value}}}})
				continue
			}
			reply(map[string]any{"bindings": []any{map[string]any{"componentId": "not-granted", "environment": map[string]string{"TOKEN": "secret"}}}})
		case "auth/cancel":
			reply(map[string]string{"state": "cancelled"})
			os.Exit(0)
		default:
			fmt.Fprintln(os.Stderr, "unexpected")
			os.Exit(2)
		}
	}
	os.Exit(0)
}

func TestBridgeChallengesAreBoundToSessionAndStatusVerified(t *testing.T) {
	driver := testDriver(t, "ready")
	account, err := driver.Status(context.Background(), true)
	if err != nil || !account.Available || account.ID != "public-user" {
		t.Fatalf("status: %+v %v", account, err)
	}
	challenges := 0
	if err := driver.LoginSession(context.Background(), "session-one", nil, func(device authorization.Device) error {
		challenges++
		if device.ChallengeID != "challenge-one" || device.Actions[0].Value != "ONE" {
			t.Fatal("invalid challenge")
		}
		return nil
	}); err != nil || challenges != 1 {
		t.Fatalf("login: %d %v", challenges, err)
	}
	forged := testDriver(t, "forged")
	if err := forged.LoginSession(context.Background(), "session-one", nil, func(authorization.Device) error { t.Fatal("foreign challenge exposed"); return nil }); err == nil {
		t.Fatal("foreign session accepted")
	}
}

func TestBridgeSeparatesDependencyPreparationFromAuthorizationFailure(t *testing.T) {
	for _, mode := range []string{"dependency-state", "unknown-failure", "foreign-failure"} {
		t.Run(mode, func(t *testing.T) {
			err := testDriver(t, mode).LoginSession(context.Background(), "session-one", nil, func(authorization.Device) error {
				t.Fatal("failed preparation exposed a challenge")
				return nil
			})
			want := status_error.PluginDependencyUnavailable
			if mode == "unknown-failure" {
				want = status_error.PluginAuthorizationFailed
			} else if mode == "foreign-failure" {
				want = status_error.PluginForbidden
			}
			if err != want {
				t.Fatalf("failure kind: got %v, want %v", err, want)
			}
		})
	}
	if _, err := testDriver(t, "dependency-rpc").Status(context.Background(), true); err != status_error.PluginDependencyUnavailable {
		t.Fatalf("status preparation failure: %v", err)
	}
}

func TestBridgeCancellationStopsProcessAndRejectsSecretScopeEscape(t *testing.T) {
	driver := testDriver(t, "cancel")
	ctx, cancel := context.WithCancel(context.Background())
	start := time.Now()
	err := driver.LoginSession(ctx, "session-one", nil, func(authorization.Device) error { cancel(); return nil })
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("cancel did not stop bridge: %v", err)
	}
	if _, err := driver.Resolve(context.Background(), []Component{{ComponentID: "allowed", Key: "skills/a", Target: "skill"}}); err == nil {
		t.Fatal("ungranted secret binding accepted")
	}
}

func TestBridgeChallengeNoticeIsPluginDeclared(t *testing.T) {
	t.Setenv("NEUXALLY_NOTICE_JSON", `{"messages":{"en":"Scan again to authorize permissions","zh-CN":"首次扫码完成后，还需再次扫码授权权限"}}`)
	var messages map[string]string
	err := testDriver(t, "notice").LoginSession(context.Background(), "session-one", nil, func(device authorization.Device) error {
		raw, _ := json.Marshal(device)
		var projected struct {
			// Notice 是通用授权提示协议。
			Notice *struct {
				// Messages 是插件声明的本地化文案。
				Messages map[string]string `json:"messages"`
			} `json:"notice"`
		}
		_ = json.Unmarshal(raw, &projected)
		if projected.Notice != nil {
			messages = projected.Notice.Messages
		}
		return nil
	})
	if err != nil || messages["zh-CN"] != "首次扫码完成后，还需再次扫码授权权限" || messages["en"] != "Scan again to authorize permissions" {
		t.Fatalf("plugin challenge notice missing: %+v %v", messages, err)
	}
}

func TestBridgeRejectsInvalidChallengeNotices(t *testing.T) {
	for name, raw := range map[string]string{
		"empty": `{}`, "null": `null`, "no-messages": `{"messages":{}}`,
		"unknown-locale":   `{"messages":{"fr":"Bonjour"}}`,
		"too-many-locales": `{"messages":{"en":"one","zh-CN":"二","fr":"trois"}}`,
		"blank":            `{"messages":{"en":" \n\t"}}`,
		"NUL":              `{"messages":{"en":"bad\u0000text"}}`,
		"too-long":         `{"messages":{"en":"` + strings.Repeat("x", 2049) + `"}}`,
		"invalid-UTF8":     "{\"messages\":{\"en\":\"" + string([]byte{255}) + "\"}}",
		"unknown-field":    `{"messages":{"en":"hello"},"tone":"warning"}`,
		"wrong-type":       `{"messages":{"en":2}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("NEUXALLY_NOTICE_JSON", raw)
			reported := false
			err := testDriver(t, "notice").LoginSession(context.Background(), "session-one", nil, func(authorization.Device) error { reported = true; return nil })
			if err == nil || reported {
				t.Fatalf("invalid notice reached a challenge: reported=%v error=%v", reported, err)
			}
		})
	}
}

func TestBridgePreservesQRPayloadAndRejectsInvalidImages(t *testing.T) {
	pngData := testQRPNG(t, 192, 192)
	for _, value := range []string{"custom://official?token=a%2Fb", constants.PluginAuthQRPNGPrefix + base64.StdEncoding.EncodeToString(pngData)} {
		t.Setenv("NEUXALLY_QR_VALUE", value)
		var actual string
		err := testDriver(t, "qr").LoginSession(context.Background(), "session-one", nil, func(device authorization.Device) error {
			actual = device.Actions[0].Value
			return nil
		})
		if err != nil || actual != value {
			t.Fatalf("QR payload was not preserved: %v", err)
		}
	}
	for _, value := range []string{"data:image/svg+xml;base64,PHN2Zy8+", constants.PluginAuthQRPNGPrefix + base64.StdEncoding.EncodeToString(pngData[:len(pngData)-1])} {
		t.Setenv("NEUXALLY_QR_VALUE", value)
		reported := false
		err := testDriver(t, "qr").LoginSession(context.Background(), "session-one", nil, func(authorization.Device) error { reported = true; return nil })
		if err == nil || reported {
			t.Fatalf("invalid QR image reached a challenge: reported=%v error=%v", reported, err)
		}
	}
}

func TestQRImagesAreBoundedAndFullyDecoded(t *testing.T) {
	encode := func(data []byte) string {
		return constants.PluginAuthQRPNGPrefix + base64.StdEncoding.EncodeToString(data)
	}
	valid := testQRPNG(t, constants.PluginAuthQRPNGMaxDimension, constants.PluginAuthQRPNGMaxDimension)
	if !safeQRValue(encode(valid)) {
		t.Fatal("valid PNG at dimension limit rejected")
	}
	corrupt := bytes.Clone(valid)
	corrupt[len(corrupt)-1] ^= 1
	for name, value := range map[string]string{
		"empty":            "",
		"invalid-base64":   constants.PluginAuthQRPNGPrefix + "%%%",
		"base64-newline":   constants.PluginAuthQRPNGPrefix + "\n" + base64.StdEncoding.EncodeToString(valid),
		"non-png":          encode([]byte("not a PNG")),
		"svg":              "data:image/svg+xml;base64,PHN2Zy8+",
		"other-mime":       "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(valid),
		"oversize":         encode(make([]byte, constants.PluginAuthQRPNGMaxBytes+1)),
		"too-wide":         encode(testQRPNG(t, constants.PluginAuthQRPNGMaxDimension+1, 1)),
		"too-high":         encode(testQRPNG(t, 1, constants.PluginAuthQRPNGMaxDimension+1)),
		"truncated":        encode(valid[:len(valid)-1]),
		"corrupt-checksum": encode(corrupt),
		"trailing-data":    encode(append(bytes.Clone(valid), 'x')),
	} {
		t.Run(name, func(t *testing.T) {
			if safeQRValue(value) {
				t.Fatal("invalid QR image accepted")
			}
		})
	}
}

func testQRPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image.NewGray(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestAuthorizationPreparationDeadlineAndCancellation(t *testing.T) {
	t.Run("cold-asset-preparation-exceeds-old-deadline", func(t *testing.T) {
		t.Parallel()
		driver := testDriver(t, "slow-preparation")
		events := make(chan authorization.Session, 4)
		authorizer := authorization.New(authorization.Config{Driver: driver, Changed: func(session authorization.Session) { events <- session }})
		authorizer.Open(context.Background())
		t.Cleanup(authorizer.Close)
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		started := time.Now()
		if _, err := authorizer.Login(ctx); err != nil {
			t.Fatalf("cold preparation failed: %v", err)
		}
		challenged := false
		for {
			select {
			case event := <-events:
				if event.ChallengeID != "" {
					challenged = true
				}
				if event.State == constants.PluginAuthorizationStateReady {
					if !challenged || time.Since(started) < 30*time.Second {
						t.Fatal("fixture did not exercise a challenge after the old deadline")
					}
					return
				}
			case <-ctx.Done():
				t.Fatalf("prepared authorization did not finish: %+v", authorizer.Snapshot())
			}
		}
	})
	t.Run("cancel-before-first-challenge", func(t *testing.T) {
		t.Parallel()
		driver := testDriver(t, "preparing")
		authorizer := authorization.New(authorization.Config{Driver: driver})
		authorizer.Open(context.Background())
		t.Cleanup(authorizer.Close)
		done := make(chan error, 1)
		go func() { _, err := authorizer.Login(context.Background()); done <- err }()
		deadline := time.Now().Add(3 * time.Second)
		for {
			if _, err := os.Stat(filepath.Join(driver.config.DataDirectory, "preparing")); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("bridge did not enter preparation")
			}
			time.Sleep(10 * time.Millisecond)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := authorizer.CancelAndWait(ctx, authorizer.Snapshot().ID); err != nil {
			t.Fatalf("preparation cancellation did not wait for process exit: %v", err)
		}
		select {
		case err := <-done:
			if err == nil || authorizer.Snapshot().State != constants.PluginAuthorizationStateCancelled {
				t.Fatalf("cancelled preparation succeeded: %v", err)
			}
		case <-ctx.Done():
			t.Fatal("preparation cancellation waited for initial deadline")
		}
	})
	t.Run("challenge-replaces-preparation-deadline", func(t *testing.T) {
		t.Parallel()
		driver := testDriver(t, "short-challenge")
		authorizer := authorization.New(authorization.Config{Driver: driver})
		authorizer.Open(context.Background())
		t.Cleanup(authorizer.Close)
		if _, err := authorizer.Login(context.Background()); err != nil {
			t.Fatalf("short challenge was not published: %v", err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			snapshot := authorizer.Snapshot()
			if snapshot.State == constants.PluginAuthorizationStateExpired {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("challenge retained preparation deadline: %+v", authorizer.Snapshot())
	})
}

func TestStatusReadDoesNotPreparePrivateDelivery(t *testing.T) {
	driver := testDriver(t, "ready")
	prepared := 0
	driver.config.PrepareDelivery = func(context.Context, *Driver, authorization.Account) error { prepared++; return nil }
	if _, err := driver.Status(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if prepared != 0 {
		t.Fatal("status read wrote private delivery material outside authorization ownership")
	}
}

func TestStageOneBridgeRejectsOldProtocolAndForeignContext(t *testing.T) {
	for _, mode := range []string{"wrong-protocol", "wrong-context"} {
		t.Run(mode, func(t *testing.T) {
			if _, err := testDriver(t, mode).Status(context.Background(), true); !errors.Is(err, status_error.PluginAuthorizationFailed) {
				t.Fatal("invalid initialization accepted", err)
			}
		})
	}
	if err := testDriver(t, "foreign-operation").LoginSession(context.Background(), "session-one", nil, func(authorization.Device) error { t.Fatal("old operation reached UI"); return nil }); !errors.Is(err, status_error.PluginForbidden) {
		t.Fatal("foreign operation notification accepted", err)
	}
}

func TestStageOneSkillEnvironmentIsLimitedToDeclaredKeysAndPrivatePaths(t *testing.T) {
	cases := []struct {
		// name 是协议替身行为。
		name string
		// keys 是允许的环境键。
		keys []string
		// paths 是必须留在私有目录的键。
		paths []string
		// target 是实际交付组件。
		target string
		// allowed 表示返回材料应可接受。
		allowed bool
	}{
		{"skill-env", []string{"TOKEN"}, nil, "skill", true},
		{"skill-env", nil, nil, "skill", false},
		{"skill-env", []string{"TOKEN"}, nil, "mcp", false},
		{"skill-env-private", []string{"CACHE"}, []string{"CACHE"}, "skill", true},
		{"skill-env-escape", []string{"CACHE"}, []string{"CACHE"}, "skill", false},
		{"skill-env-home", []string{"HOME"}, nil, "skill", false},
	}
	for _, tc := range cases {
		t.Run(tc.name+tc.target+strings.Join(tc.keys, ","), func(t *testing.T) {
			driver := testDriver(t, tc.name)
			_, err := driver.Resolve(context.Background(), []Component{{ComponentID: "skill-one", Key: "skills/a", Target: tc.target, EnvironmentKeys: tc.keys, PrivatePathKeys: tc.paths}})
			if tc.allowed && err != nil {
				t.Fatal(err)
			}
			if !tc.allowed && !errors.Is(err, status_error.PluginForbidden) {
				t.Fatalf("undeclared/escaping environment accepted: %v", err)
			}
		})
	}
}

func TestBridgeDrainsTerminalFramesBeforeProcessEOF(t *testing.T) {
	for attempt := 0; attempt < 5; attempt++ {
		driver := testDriver(t, "terminal-exit")
		err := driver.LoginSession(context.Background(), "session", nil, func(authorization.Device) error {
			// 让测试进程先交付终态并关闭 stdout，覆盖缓冲通知与 EOF 同时就绪。
			time.Sleep(50 * time.Millisecond)
			return nil
		})
		if err != nil {
			t.Fatalf("terminal authorization frame discarded at EOF: %v", err)
		}
	}
	driver := testDriver(t, "dependency-rpc-exit")
	if _, err := driver.Status(context.Background(), true); !errors.Is(err, status_error.PluginDependencyUnavailable) {
		t.Fatalf("terminal RPC failure lost its stable error: %v", err)
	}
}
