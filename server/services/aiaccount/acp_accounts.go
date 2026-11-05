package aiaccount

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
	"github.com/JieWaZi/acp-go/pkg/agent_next/qoder"
)

type acpSystemAccount struct {
	// configHome identifies the canonical native settings and session root.
	configHome string
	// userHome retains the native OS home for stores outside the settings root.
	userHome string
	// fingerprint binds the canonical and lexical OS home to the frozen selection.
	fingerprint string
	// environment carries the authoritative launch scope and selected credential.
	environment map[string]string
	// qoderUserID is private native evidence for account-scoped management reads.
	qoderUserID string
	// qoderLoginVerified records a successful native login identity read.
	qoderLoginVerified bool
}

func (s *Service) resolveACPSystemAccount(ctx context.Context, request agent.AccountRequest, configHome string) (agent.AccountRequest, error) {
	if request.AccountID != agent.SystemAccountID {
		return agent.AccountRequest{}, status_error.AIAccountConfigurationInvalid
	}
	configHome = acpSystemConfigOverride(request.Provider, configHome)
	config, err := newACPSystemAccount(request, configHome)
	if err != nil {
		return agent.AccountRequest{}, err
	}
	subject, err := s.acpSystemSubject(ctx, request, &config)
	if err != nil {
		return agent.AccountRequest{}, err
	}
	request.ConfigHome, request.NativeConfigHome = config.configHome, configHome
	request.ConfigFingerprint = config.fingerprint
	request.Subject, request.AuthGeneration = subject, systemAccountGeneration
	return request, nil
}

func (s *Service) acquireACPSystemAccount(ctx context.Context, request agent.AccountRequest) (agent.AccountLease, error) {
	if request.AccountID != agent.SystemAccountID || request.Subject == "" ||
		request.AuthGeneration != systemAccountGeneration || !filepath.IsAbs(request.ConfigHome) {
		return agent.AccountLease{}, status_error.AIAccountConfigurationInvalid
	}
	recheck := func(ctx context.Context) (acpSystemAccount, error) {
		config, err := newACPSystemAccount(request, request.NativeConfigHome)
		if err != nil {
			return acpSystemAccount{}, err
		}
		if config.configHome != request.ConfigHome || config.fingerprint != request.ConfigFingerprint {
			return acpSystemAccount{}, status_error.RuntimeConfigurationInvalid
		}
		subject, err := s.acpSystemSubject(ctx, request, &config)
		if err != nil {
			return acpSystemAccount{}, err
		}
		if subject != request.Subject {
			return acpSystemAccount{}, status_error.RuntimeAuthRequired
		}
		return config, nil
	}
	config, err := recheck(ctx)
	if err != nil {
		return agent.AccountLease{}, err
	}
	return agent.AccountLease{
		ConfigHome: config.configHome, Environment: config.environment, QoderUserID: config.qoderUserID,
		QoderLoginVerified: config.qoderLoginVerified,
		VerifyLogin: func(ctx context.Context) error {
			_, err := recheck(ctx)
			return err
		},
	}, nil
}

func newACPSystemAccount(request agent.AccountRequest, nativeConfigHome string) (acpSystemAccount, error) {
	for _, argument := range request.PrefixArgs {
		option, _, _ := strings.Cut(argument, "=")
		switch option {
		case "--config-dir", "--sdk-auth-file", "--settings", "--setting-sources",
			"--cwd", "-w", "--worktree", "--add-dir":
			return acpSystemAccount{}, status_error.RuntimeConfigurationInvalid
		}
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return acpSystemAccount{}, status_error.RuntimeConfigurationInvalid
	}
	canonicalHome, err := systemAccountHome(userHome, false)
	if err != nil {
		return acpSystemAccount{}, err
	}
	configHome, err := acpSystemConfigRoot(request.Provider, userHome, nativeConfigHome)
	if err != nil {
		return acpSystemAccount{}, err
	}
	homeVariable := "HOME"
	if runtime.GOOS == "windows" {
		homeVariable = "USERPROFILE"
	}
	config := acpSystemAccount{
		configHome: configHome, userHome: userHome,
		fingerprint: accountSubject("native-acp-system-home-v1", request.Provider, canonicalHome, userHome),
		environment: map[string]string{
			accountconfig.ACPSystemConfigVariable(request.Provider): nativeConfigHome,
			homeVariable: userHome,
		},
	}
	if request.Provider == constants.RuntimeIDWorkBuddy {
		// The explicit token has higher priority than the model API key.
		token := os.Getenv("CODEBUDDY_AUTH_TOKEN")
		if token != "" {
			config.environment["CODEBUDDY_AUTH_TOKEN"] = token
		} else if key := os.Getenv("CODEBUDDY_API_KEY"); key != "" {
			config.environment["CODEBUDDY_API_KEY"] = key
		}
		region := os.Getenv("CODEBUDDY_INTERNET_ENVIRONMENT")
		switch region {
		case "", "external", "internal", "ioa":
			config.environment["CODEBUDDY_INTERNET_ENVIRONMENT"] = region
		default:
			return acpSystemAccount{}, status_error.RuntimeConfigurationInvalid
		}
		return config, nil
	}
	if err := config.checkQoderRegionalRoot(request.Provider); err != nil {
		return acpSystemAccount{}, err
	}
	tokenVariable := "QODER_PERSONAL_ACCESS_TOKEN"
	if request.Provider == constants.RuntimeIDQoderCN {
		tokenVariable = "QODERCN_PERSONAL_ACCESS_TOKEN"
	}
	if token := os.Getenv(tokenVariable); token != "" {
		config.environment[tokenVariable] = token
	}
	return config, nil
}

func acpSystemConfigOverride(provider, configuredHome string) string {
	if configuredHome != "" {
		return configuredHome
	}
	return os.Getenv(accountconfig.ACPSystemConfigVariable(provider))
}

func acpSystemConfigRoot(provider, userHome, configuredHome string) (string, error) {
	if configuredHome != "" {
		return systemAccountHome(configuredHome, true)
	}
	var defaultDir string
	switch provider {
	case constants.RuntimeIDWorkBuddy:
		defaultDir = ".codebuddy"
	case constants.RuntimeIDQoder:
		defaultDir = ".qoder"
	case constants.RuntimeIDQoderCN:
		defaultDir = ".qoder-cn"
	default:
		return "", status_error.RuntimeNotRegistered
	}
	return systemAccountHome(filepath.Join(userHome, defaultDir), true)
}

func (c acpSystemAccount) checkQoderRegionalRoot(provider string) error {
	peer := accountconfig.QoderPeerRuntimeID(provider)
	for _, override := range []string{"", acpSystemConfigOverride(peer, "")} {
		// An invalid directory in the other installation cannot alias this root.
		other, err := acpSystemConfigRoot(peer, c.userHome, override)
		if err == nil && sameACPConfigRoot(c.configHome, other) {
			return status_error.RuntimeConfigurationInvalid
		}
	}
	return nil
}

// ValidateQoderConfigHomes applies the runtime's native root rule before saving
// a selection and checks the other edition's independently saved custom roots.
func ValidateQoderConfigHomes(provider, configuredHome string, peerConfiguredHomes []string) error {
	peer := accountconfig.QoderPeerRuntimeID(provider)
	if peer == "" {
		return status_error.RuntimeNotRegistered
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return status_error.RuntimeConfigurationInvalid
	}
	home, err := acpSystemConfigRoot(provider, userHome, acpSystemConfigOverride(provider, configuredHome))
	if err != nil {
		return err
	}
	account := acpSystemAccount{configHome: home, userHome: userHome}
	if err := account.checkQoderRegionalRoot(provider); err != nil {
		return err
	}
	for _, peerHome := range peerConfiguredHomes {
		other, err := acpSystemConfigRoot(peer, userHome, peerHome)
		if err == nil && sameACPConfigRoot(home, other) {
			return status_error.RuntimeConfigurationInvalid
		}
	}
	return nil
}

func sameACPConfigRoot(first, second string) bool {
	if relative, err := filepath.Rel(first, second); err == nil && relative == "." {
		return true
	}
	// Existing aliases can name the same directory under different spellings
	// on case-insensitive filesystems and mounted platform volumes.
	firstInfo, firstErr := os.Stat(first)
	secondInfo, secondErr := os.Stat(second)
	return firstErr == nil && secondErr == nil && os.SameFile(firstInfo, secondInfo)
}

func (s *Service) acpSystemSubject(ctx context.Context, request agent.AccountRequest, c *acpSystemAccount) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if request.Provider == constants.RuntimeIDWorkBuddy {
		return c.workBuddySubject(request.CWD)
	}
	for _, key := range []string{"QODER_PERSONAL_ACCESS_TOKEN", "QODERCN_PERSONAL_ACCESS_TOKEN"} {
		if token := c.environment[key]; token != "" {
			// The PAT is exact launch evidence. The CLI status command can describe
			// a different saved login, so it cannot authenticate this PAT for us.
			return accountSubject(request.Provider+"-system-pat-v1", c.configHome, token), nil
		}
	}
	options := qoder.Options{
		RuntimeID: request.Provider, Executable: request.Executable, PrefixArgs: request.PrefixArgs,
		Environment: nativeAccountEnvironment(c.environment), WorkingDirectory: request.CWD,
	}
	scope, key := c.qoderSavedLoginKey(request)
	s.mutex.Lock()
	previous := s.qoderLogins[request.Provider]
	s.mutex.Unlock()
	if key != "" && previous.key == key && time.Since(previous.observedAt) < 30*time.Second {
		c.qoderUserID, c.qoderLoginVerified = previous.userID, true
		return previous.subject, nil
	}
	identity, identityErr := qoder.ReadNativeIdentity(ctx, options)
	if identityErr == nil && identity.UserID != "" && key != "" {
		if currentScope, currentKey := c.qoderSavedLoginKey(request); currentKey != key {
			// A native refresh can legitimately replace the saved credential.
			// Re-read once against the new snapshot before accepting its subject.
			if currentKey == "" {
				return "", status_error.RuntimeUnavailable
			}
			scope, key = currentScope, currentKey
			identity, identityErr = qoder.ReadNativeIdentity(ctx, options)
			if _, currentKey := c.qoderSavedLoginKey(request); currentKey != key {
				return "", status_error.RuntimeUnavailable
			}
		}
	}
	if identityErr == nil && identity.UserID != "" {
		c.qoderUserID, c.qoderLoginVerified = identity.UserID, true
		subject := accountSubject(request.Provider+"-system-login-v1", c.configHome, identity.UserID, identity.Organization)
		if key != "" {
			s.mutex.Lock()
			if s.qoderLogins == nil {
				s.qoderLogins = make(map[string]qoderLoginObservation)
			}
			s.qoderLogins[request.Provider] = qoderLoginObservation{
				scope: scope, key: key, subject: subject, userID: identity.UserID, observedAt: time.Now(),
			}
			s.mutex.Unlock()
		}
		return subject, nil
	}
	// The user status contains display information but no user ID. The
	// service-account status instead has an explicit native principal and scope.
	statusScope, statusKey := c.qoderSavedLoginKey(request)
	login, statusErr := qoder.ReadLoginStatus(ctx, options)
	if statusErr == nil {
		if !login.Authenticated {
			s.mutex.Lock()
			if statusScope != "" && previous.scope == statusScope && s.qoderLogins[request.Provider] == previous {
				delete(s.qoderLogins, request.Provider)
			}
			s.mutex.Unlock()
			return "", status_error.RuntimeAuthRequired
		}
		if login.AccountType == "service_account" && login.PrincipalID != "" {
			if statusKey != "" {
				if _, currentKey := c.qoderSavedLoginKey(request); currentKey != statusKey {
					return "", status_error.RuntimeUnavailable
				}
			}
			c.qoderLoginVerified = true
			// The native owner identity includes organization when the principal reports it.
			return accountSubject(request.Provider+"-system-service-account-v1", c.configHome, login.PrincipalID, login.OrganizationID), nil
		}
	}
	if identityErr == nil && statusErr == nil {
		return "", status_error.RuntimeUnavailable
	}
	return "", nativeAccountStatusError(ctx, request.Provider, errors.Join(identityErr, statusErr))
}

type qoderLoginObservation struct {
	// scope is the selected regional launcher and configuration root.
	scope string
	// key binds this receipt to the unchanged native credential and launch scope.
	key string
	// subject is the hashed native account and organization identity.
	subject string
	// userID is the exact native ID used to fence management reads.
	userID string
	// observedAt bounds reuse of previously verified login evidence.
	observedAt time.Time
}

// qoderSavedLoginKey detects local changes without decoding native credentials.
// Qoder 1.1.53 restores the encrypted user file with its machine-specific key.
// The fingerprint can only reuse a native identity receipt for these exact
// bytes, never establish a subject or prove a sign-in by itself.
func (c acpSystemAccount) qoderSavedLoginKey(request agent.AccountRequest) (scope, key string) {
	if !filepath.IsAbs(request.Executable) {
		return "", ""
	}
	executable, err := filepath.EvalSymlinks(request.Executable)
	if err != nil {
		return "", ""
	}
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() {
		return "", ""
	}
	facts := []string{c.configHome, c.fingerprint, c.environment[accountconfig.ACPSystemConfigVariable(request.Provider)],
		request.CWD, executable, strconv.FormatInt(info.Size(), 10), strconv.FormatInt(info.ModTime().UnixNano(), 10)}
	scope = accountSubject(request.Provider+"-login-scope-v1", append(facts, request.PrefixArgs...)...)

	root := filepath.Join(c.configHome, ".auth")
	auth, err := os.Lstat(root)
	if err != nil || !auth.IsDir() {
		return scope, ""
	}
	facts = []string{scope}
	for _, name := range []string{"user", "machine_id"} {
		data, err := readNativeAccountFile(filepath.Join(root, name))
		if err != nil || len(data) == 0 {
			return scope, ""
		}
		sum := sha256.Sum256(data)
		clear(data)
		facts = append(facts, hex.EncodeToString(sum[:]))
	}
	return scope, accountSubject(request.Provider+"-saved-login-evidence-v1", facts...)
}

func (c acpSystemAccount) workBuddySubject(cwd string) (string, error) {
	if err := validateWorkBuddyCredentialSources(c.configHome, cwd); err != nil {
		return "", err
	}
	region := c.environment["CODEBUDDY_INTERNET_ENVIRONMENT"]
	for _, key := range []string{"CODEBUDDY_AUTH_TOKEN", "CODEBUDDY_API_KEY"} {
		if credential := c.environment[key]; credential != "" {
			return accountSubject("workbuddy-system-env-v1", c.configHome, region, key, credential), nil
		}
	}
	// CodeBuddy Code 2.151.0 stores its default CLI OAuth account in shared
	// platform data, independently of CODEBUDDY_CONFIG_DIR. This identifies a
	// locally saved principal. It does not attest that the token is valid.
	authHome, err := systemAccountHome(filepath.Join(workBuddyPlatformData(c.userHome), "CodeBuddyExtension", "Data", "Public", "auth"), true)
	if err != nil {
		return "", err
	}
	path := filepath.Join(authHome, "Tencent-Cloud.coding-copilot.info")
	if err := checkWorkBuddyLogoutMarker(path); err != nil {
		return "", err
	}
	data, err := readNativeAccountFile(path)
	if err != nil {
		return "", err
	}
	defer clear(data)
	var stored struct {
		// Account contains the selected vendor subject and enterprise scope.
		Account struct {
			// UID is the provider account identifier exposed by the native client.
			UID json.RawMessage `json:"uid"`
			// EnterpriseID distinguishes identities in different enterprise scopes.
			EnterpriseID json.RawMessage `json:"enterpriseId"`
			// Type identifies the credential realm reported by the vendor.
			Type string `json:"type"`
		} `json:"account"`
		// Auth holds opaque local credential material.
		Auth struct {
			// AccessToken is checked only for local presence, including encrypted values.
			AccessToken json.RawMessage `json:"accessToken"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		return "", status_error.AIAccountCredentialError
	}
	defer clear(stored.Auth.AccessToken)
	uid := nativeIdentifier(stored.Account.UID)
	if uid == "" || !workBuddyAccessTokenPresent(stored.Auth.AccessToken) {
		return "", status_error.RuntimeAuthRequired
	}
	if err := checkWorkBuddyLogoutMarker(path); err != nil {
		return "", err
	}
	return accountSubject("workbuddy-system-login-v1", path, region, stored.Account.Type, uid, nativeIdentifier(stored.Account.EnterpriseID)), nil
}

func workBuddyAccessTokenPresent(value json.RawMessage) bool {
	value = bytes.TrimSpace(value)
	// Native credentials may be plaintext strings or codec envelopes.
	return len(value) > 2 && (value[0] == '"' || value[0] == '{')
}

func workBuddyPlatformData(home string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support")
	case "windows":
		return filepath.Join(home, "AppData", "Local")
	default:
		return filepath.Join(home, ".local", "share")
	}
}

func checkWorkBuddyLogoutMarker(path string) error {
	_, err := os.Lstat(path + ".logged-out")
	switch {
	case err == nil:
		return status_error.RuntimeAuthRequired
	case errors.Is(err, os.ErrNotExist):
		return nil
	default:
		return status_error.AIAccountCredentialError
	}
}

func validateWorkBuddyCredentialSources(home, cwd string) error {
	if !filepath.IsAbs(cwd) {
		return status_error.RuntimeConfigurationInvalid
	}
	for _, file := range []string{
		filepath.Join(home, "settings.json"),
		filepath.Join(cwd, ".codebuddy", "settings.json"),
		filepath.Join(cwd, ".codebuddy", "settings.local.json"),
	} {
		if err := validateWorkBuddyCredentialSettings(file); err != nil {
			return err
		}
	}
	return nil
}

func validateWorkBuddyCredentialSettings(path string) error {
	data, err := readNativeAccountFile(path)
	if errors.Is(err, status_error.RuntimeAuthRequired) {
		return nil
	}
	if err != nil {
		return err
	}
	defer clear(data)
	return accountconfig.ValidateWorkBuddyCredentialSettings(data)
}

func nativeIdentifier(value json.RawMessage) string {
	var text string
	if json.Unmarshal(value, &text) == nil {
		return strings.TrimSpace(text)
	}
	var number json.Number
	if json.Unmarshal(value, &number) == nil {
		return number.String()
	}
	return ""
}
