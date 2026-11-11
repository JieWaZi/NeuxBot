package agent

import (
	"context"
	"errors"
	"net/url"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// NativeAuthenticationProgressClient reports only public native authorization details.
// The handler runs synchronously while Authenticate owns the native process.
type NativeAuthenticationProgressClient interface {
	// SetAuthenticationProgressHandler receives each native device authorization update.
	SetAuthenticationProgressHandler(func(verificationURL, userCode string))
}

// kimiAuthenticationClient owns CLI device login and the subsequent auth-gated ACP discovery.
type kimiAuthenticationClient struct {
	// account retains the selected system scope, including an explicit native config root.
	account AccountRequest
	// launch retains the exact installed program and static arguments.
	launch ProviderLaunch
	// environment preserves only the selected native credentials and home overrides.
	environment map[string]string
	// progress receives native device authorization details without raw process output.
	progress func(string, string)
	// verifier remains owned here until its native process confirms cleanup.
	verifier *nativeAuthenticationClient
}

func (c *kimiAuthenticationClient) SetAuthenticationProgressHandler(handler func(string, string)) {
	c.progress = handler
}

func (c *kimiAuthenticationClient) command(ctx context.Context, args ...string) (*exec.Cmd, error) {
	env, err := nativeLaunchEnvironment(constants.RuntimeIDKimi, c.launch, AccountLease{Environment: c.environment}, LaunchAuthLease{}, ResourceLease{})
	if err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, c.launch.Executable, append(slices.Clone(c.launch.PrefixArgs), args...)...)
	command.Dir, command.Env, command.WaitDelay = c.account.CWD, env, 2*time.Second
	configureAuthenticationProcess(command)
	return command, nil
}

func (c *kimiAuthenticationClient) Initialize(ctx context.Context) (NativeAuthenticationCapabilities, error) {
	command, err := c.command(ctx, "login", "--help")
	if err != nil {
		return NativeAuthenticationCapabilities{}, err
	}
	output := &kimiAuthenticationOutput{}
	command.Stdout, command.Stderr = output, output
	if err := command.Run(); err != nil {
		return NativeAuthenticationCapabilities{}, kimiAuthenticationError(ctx, err)
	}
	help := strings.ToLower(output.buffer)
	if !strings.Contains(help, "kimi login") || (!strings.Contains(help, "device-code") && !strings.Contains(help, "device code")) {
		return NativeAuthenticationCapabilities{}, status_error.RuntimePolicyUnsupported
	}
	return NativeAuthenticationCapabilities{EnvironmentCredential: systemAuthenticationHasCredential(constants.RuntimeIDKimi, c.environment), Methods: []NativeAuthenticationMethod{{ID: constants.KimiLoginAuthMethod, Name: "Kimi", Kind: "agent"}}}, nil
}

func (c *kimiAuthenticationClient) Authenticate(ctx context.Context, method string) (constants.RuntimeAuthenticationResult, error) {
	if method != constants.KimiLoginAuthMethod {
		return constants.RuntimeAuthenticationResultUnspecified, status_error.RuntimePolicyUnsupported
	}
	command, err := c.command(ctx, "login")
	if err != nil {
		return constants.RuntimeAuthenticationResultUnspecified, err
	}
	output := &kimiAuthenticationOutput{progress: c.progress}
	command.Stdout, command.Stderr = output, output
	if err := command.Run(); err != nil {
		return constants.RuntimeAuthenticationResultUnspecified, kimiAuthenticationError(ctx, err)
	}
	// Start a fresh management process so it reads the credentials just written by the CLI.
	client, err := newDirectAgentClient(ctx, c.account, c.launch, AccountLease{Environment: c.environment}, LaunchAuthLease{}, ResourceLease{}, AgentClientCallbacks{})
	if err != nil {
		return constants.RuntimeAuthenticationResultUnspecified, err
	}
	c.verifier = &nativeAuthenticationClient{provider: constants.RuntimeIDKimi, client: client}
	if _, err := c.verifier.Initialize(ctx); err != nil {
		return constants.RuntimeAuthenticationResultUnspecified, err
	}
	if err := client.verifyKimiLogin(ctx); err != nil {
		return constants.RuntimeAuthenticationResultUnspecified, err
	}
	return constants.RuntimeAuthenticationResultAuthenticationCompleted, nil
}

func (c *kimiAuthenticationClient) Close(ctx context.Context) error {
	if c.verifier != nil {
		if err := c.verifier.Close(ctx); err != nil {
			return err
		}
		c.verifier = nil
	}
	return nil
}

func (c *directAgentClient) verifyKimiLogin(ctx context.Context) error {
	// Kimi 2.x advertises terminal auth, so the shared adapter cannot call its
	// authenticate method. On a fresh management connection ReadModels creates
	// an empty discovery session behind Kimi's native ensureAuthed gate, then
	// closes it. No prompt, tools or public chat session is created.
	_, err := c.ReadModels(ctx)
	return err
}

func kimiAuthenticationError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Exit status and output may contain credentials; never retain them in logs or responses.
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return status_error.RuntimeAuthRequired
	}
	logx.Errorf("Kimi login process failed: error_type=%T", err)
	return status_error.RuntimeUnavailable
}

var kimiAuthenticationANSI = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
var kimiAuthenticationURL = regexp.MustCompile(`https://[^\s\x1b]+`)
var kimiAuthenticationDeviceCode = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,127}$`)

// kimiAuthenticationOutput retains at most one bounded partial line and no process log.
type kimiAuthenticationOutput struct {
	// buffer retains incomplete native output or the bounded help response.
	buffer string
	// progress publishes complete official device addresses, including renewed codes.
	progress func(string, string)
	// lastAddress prevents duplicate events while allowing the CLI to renew an expired device code.
	lastAddress string
	// lastCode distinguishes renewed codes when the native verification URI stays unchanged.
	lastCode string
	// pendingAddress binds the native URI fallback to the immediately following native code line.
	pendingAddress string
}

func (w *kimiAuthenticationOutput) Write(data []byte) (int, error) {
	w.buffer += string(data)
	if len(w.buffer) > 16384 {
		w.buffer = w.buffer[len(w.buffer)-16384:]
	}
	if w.progress == nil {
		return len(data), nil
	}
	for {
		line, rest, found := strings.Cut(w.buffer, "\n")
		if !found {
			break
		}
		w.buffer = rest
		line = kimiAuthenticationANSI.ReplaceAllString(line, "")
		pending := w.pendingAddress
		w.pendingAddress = ""
		if code, found := strings.CutPrefix(line, "If the browser did not open, paste the URL above and enter code: "); found && pending != "" {
			code = strings.TrimSpace(code)
			if kimiAuthenticationDeviceCode.MatchString(code) {
				w.report(pending, code)
			}
		}
		for _, address := range kimiAuthenticationURL.FindAllString(line, -1) {
			parsed, err := url.Parse(address)
			if err != nil || parsed.User != nil || parsed.Port() != "" {
				continue
			}
			host := strings.ToLower(parsed.Hostname())
			if host != "kimi.com" && host != "kimi.ai" && !strings.HasSuffix(host, ".kimi.com") && !strings.HasSuffix(host, ".kimi.ai") {
				continue
			}
			code := parsed.Query().Get("user_code")
			if len(code) > 128 || len(address) > 2048 {
				continue
			}
			if code == "" {
				if strings.TrimSpace(line) == "Opening browser for Kimi device login: "+address && !parsed.Query().Has("user_code") {
					w.pendingAddress = address
				}
				continue
			}
			w.report(address, code)
			break
		}
	}
	return len(data), nil
}

func (w *kimiAuthenticationOutput) report(address, code string) {
	if address == w.lastAddress && code == w.lastCode {
		return
	}
	w.lastAddress, w.lastCode = address, code
	w.progress(address, code)
}
