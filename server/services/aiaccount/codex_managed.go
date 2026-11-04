package aiaccount

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

type managedCodexSelection struct {
	// request freezes identity and native configuration root.
	request agent.AccountRequest
	// account supplies selected models and the credential CAS revision.
	account models.AIAccount
	// endpoint is the selected account's validated Responses endpoint.
	endpoint models.AIAccountEndpoint
	// apiKey is populated only for a selected API key account.
	apiKey string
	// chatGPT is populated only for a selected ChatGPT subscription.
	chatGPT codexCredential
}

// ResolveCodexAccount freezes native account identity before a Thread or model query admits work.
func (s *Service) ResolveCodexAccount(ctx context.Context, request agent.AccountRequest, systemHome string) (agent.AccountRequest, error) {
	if request.AccountID == agent.SystemAccountID {
		return ResolveCodexSystemAccount(ctx, request, systemHome)
	}
	if request.Provider != constants.RuntimeIDCodex || !isManagedCodexID(request.AccountID) {
		return agent.AccountRequest{}, status_error.AIAccountConfigurationInvalid
	}
	release, err := s.acquireLease(ctx, request.AccountID, false, false)
	if err != nil {
		return agent.AccountRequest{}, err
	}
	defer release()
	unlock, err := s.credentialGate(request.AccountID).Lock(ctx)
	if err != nil {
		return agent.AccountRequest{}, err
	}
	defer unlock()
	selected, err := s.resolveManagedCodex(ctx, request)
	return selected.request, err
}

// AcquireCodexAccount pins managed credentials until the native consumer exits and cleanup succeeds.
func (s *Service) AcquireCodexAccount(ctx context.Context, request agent.AccountRequest) (agent.AccountLease, error) {
	if request.AccountID == agent.SystemAccountID {
		return AcquireCodexSystemAccount(ctx, request)
	}
	if request.Provider != constants.RuntimeIDCodex || !isManagedCodexID(request.AccountID) {
		return agent.AccountLease{}, status_error.AIAccountConfigurationInvalid
	}
	release, err := s.acquireLease(ctx, request.AccountID, false, false)
	if err != nil {
		return agent.AccountLease{}, err
	}
	lease, err := s.acquireManagedCodex(ctx, request, release)
	if err != nil {
		release()
	}
	return lease, err
}

func (s *Service) resolveManagedCodex(ctx context.Context, request agent.AccountRequest) (managedCodexSelection, error) {
	account, err := s.accounts.Get(ctx, request.AccountID)
	if err != nil {
		return managedCodexSelection{}, err
	}
	endpoint, ok := accountEndpoint(account, constants.RuntimeIDCodex)
	if !ok {
		return managedCodexSelection{}, status_error.AIAccountConfigurationInvalid
	}
	if !account.HasCredential {
		return managedCodexSelection{}, status_error.RuntimeAuthRequired
	}
	if account.AuthMode == constants.AccountAuthModeAPIKey && (len(account.SelectedModels) == 0 ||
		request.Model != "" && !slices.Contains(account.SelectedModels, request.Model)) {
		return managedCodexSelection{}, status_error.AIAccountConfigurationInvalid
	}
	secret, err := s.accounts.Credential(ctx, account.ID)
	if err != nil {
		return managedCodexSelection{}, err
	}
	if secret == "" {
		return managedCodexSelection{}, status_error.RuntimeAuthRequired
	}
	home, err := s.managedCodexHome(account, false)
	if err != nil {
		return managedCodexSelection{}, err
	}
	selected := managedCodexSelection{account: account, endpoint: endpoint}
	identity := ""
	switch account.AuthMode {
	case constants.AccountAuthModeAPIKey:
		selected.apiKey = secret
	case constants.AccountAuthModeSubscription:
		if json.Unmarshal([]byte(secret), &selected.chatGPT) != nil || !selected.chatGPT.Tokens.Valid() {
			return managedCodexSelection{}, status_error.AIAccountCredentialError
		}
		identity = selected.chatGPT.Tokens.Identity()
	default:
		return managedCodexSelection{}, status_error.AIAccountConfigurationInvalid
	}
	scope, _ := json.Marshal([]string{"codex-managed-v1", account.ID, string(account.AuthMode), identity})
	digest := sha256.Sum256(scope)
	request.Subject = "codex-managed:" + hex.EncodeToString(digest[:])
	request.ConfigHome, request.AuthGeneration = home, account.AuthGeneration
	selected.request = request
	return selected, nil
}

func (s *Service) acquireManagedCodex(ctx context.Context, request agent.AccountRequest, release func()) (agent.AccountLease, error) {
	unlock, err := s.credentialGate(request.AccountID).Lock(ctx)
	if err != nil {
		return agent.AccountLease{}, err
	}
	defer unlock()
	selected, err := s.resolveManagedCodex(ctx, request)
	if err != nil {
		return agent.AccountLease{}, err
	}
	if selected.request != request {
		return agent.AccountLease{}, status_error.RuntimeAuthRequired
	}
	account, endpoint := selected.account, selected.endpoint
	home, err := s.managedCodexHome(account, true)
	if err != nil {
		return agent.AccountLease{}, err
	}
	if home != request.ConfigHome {
		return agent.AccountLease{}, status_error.AIAccountConfigurationInvalid
	}
	spec := accountconfig.Spec{
		ID: account.ID, RuntimeID: constants.RuntimeIDCodex, AuthMode: account.AuthMode,
		Protocol: endpoint.Protocol, BaseURL: endpoint.BaseURL,
		SelectedModels: account.SelectedModels, ModelContextSizes: account.ModelContextSizes,
		ModelDisplayNames: account.ModelDisplayNames, ModelReasoningLevels: account.ModelReasoningLevels,
	}
	if account.AuthMode == constants.AccountAuthModeAPIKey {
		spec.APIKey, spec.Model = selected.apiKey, account.DefaultModel
		if !slices.Contains(account.SelectedModels, spec.Model) {
			spec.Model = account.SelectedModels[0]
		}
	}
	config, err := accountconfig.PrepareCodexHome(home, spec)
	if err != nil {
		return agent.AccountLease{}, err
	}

	lease := agent.AccountLease{ConfigHome: home, Environment: config.Environment, CodexConfig: config.Overrides}
	if account.AuthMode == constants.AccountAuthModeAPIKey {
		expected := sha256.Sum256([]byte(selected.apiKey))
		lease.CodexCredentialKind = "api_key"
		lease.VerifyCodexCredential = func(ctx context.Context, proof [sha256.Size]byte) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if subtle.ConstantTimeCompare(proof[:], expected[:]) != 1 {
				return status_error.RuntimeAuthRequired
			}
			return nil
		}
		lease.Close = func(context.Context) error { release(); return nil }
		return lease, nil
	}
	saved := selected.chatGPT
	if err := s.prepareManagedCodexAuth(ctx, account, home, saved); err != nil {
		return agent.AccountLease{}, err
	}
	identity := saved.Tokens.Identity()
	lease.CodexCredentialKind = "account"
	lease.VerifyCodexCredential = func(ctx context.Context, proof [sha256.Size]byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := readCodexChatGPTCredential(home)
		if err != nil {
			return err
		}
		expected := sha256.Sum256([]byte(current.Tokens.AccessToken))
		if current.Tokens.Identity() != identity || subtle.ConstantTimeCompare(proof[:], expected[:]) != 1 {
			return status_error.RuntimeAuthRequired
		}
		return nil
	}
	var closeMu sync.Mutex
	closed := false
	lease.Close = func(ctx context.Context) error {
		closeMu.Lock()
		defer closeMu.Unlock()
		if closed {
			return nil
		}
		if err := s.recoverManagedCodexAuth(ctx, account.ID, home, identity); err != nil {
			return err
		}
		release()
		closed = true
		return nil
	}
	return lease, nil
}

func (s *Service) prepareManagedCodexAuth(ctx context.Context, account models.AIAccount, home string, saved codexCredential) error {
	latest := saved
	current, err := readCodexChatGPTCredential(home)
	if err == nil {
		if current.Tokens.Identity() != saved.Tokens.Identity() {
			return status_error.RuntimeAuthRequired
		}
		if current.LastRefresh.After(saved.LastRefresh) {
			latest = current
		}
	} else if _, statErr := os.Lstat(filepath.Join(home, "auth.json")); !errors.Is(statErr, os.ErrNotExist) {
		return err
	}
	if account.LiveAuthPath != "" {
		var retireErr error
		latest, retireErr = s.retireLegacyCodexProjection(ctx, account, latest)
		if retireErr != nil {
			return retireErr
		}
	}
	if account.LiveAuthPath != "" || latest.LastRefresh.After(saved.LastRefresh) {
		if persistErr := s.persistManagedCodexRefresh(ctx, account, latest, ""); persistErr != nil {
			return persistErr
		}
	}
	if err == nil && !latest.LastRefresh.After(current.LastRefresh) {
		return nil
	}
	return writeCodexChatGPTCredential(home, latest)
}

// retireLegacyCodexProjection removes only an owned projection with a verified principal.
// A fresher legacy rotation reaches encrypted storage before its file is retired. The
// caller clears the pointer only after the retirement succeeds.
func (s *Service) retireLegacyCodexProjection(ctx context.Context, account models.AIAccount, latest codexCredential) (codexCredential, error) {
	legacyPath := account.LiveAuthPath
	home := filepath.Dir(legacyPath)
	workspace := filepath.Dir(filepath.Dir(filepath.Dir(home)))
	if !filepath.IsAbs(legacyPath) || filepath.Join(accountconfig.Home(workspace, constants.RuntimeIDCodex), "auth.json") != legacyPath {
		logx.Warnf("left unverified legacy Codex auth projection: account_id=%s", account.ID)
		return latest, nil
	}
	workspace, err := filepath.EvalSymlinks(workspace)
	if errors.Is(err, os.ErrNotExist) {
		return latest, nil
	}
	if err != nil {
		return latest, managedCodexStorageError(account.ID, err)
	}
	home = accountconfig.Home(workspace, constants.RuntimeIDCodex)
	canonical, err := filepath.EvalSymlinks(home)
	if errors.Is(err, os.ErrNotExist) {
		return latest, nil
	}
	if err != nil {
		return latest, managedCodexStorageError(account.ID, err)
	}
	if canonical != home {
		logx.Warnf("left unverified legacy Codex auth projection: account_id=%s", account.ID)
		return latest, nil
	}
	path := filepath.Join(home, "auth.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return latest, nil
	}
	if err != nil {
		return latest, managedCodexStorageError(account.ID, err)
	}
	if !info.Mode().IsRegular() {
		logx.Warnf("left unverified legacy Codex auth projection: account_id=%s", account.ID)
		return latest, nil
	}
	data, err := readCodexAuthFile(home)
	if errors.Is(err, status_error.RuntimeAuthRequired) {
		return latest, nil
	}
	if err != nil {
		return latest, err
	}
	defer clear(data)
	legacy, err := decodeCodexChatGPTCredential(data)
	if err != nil || legacy.Tokens.Identity() != latest.Tokens.Identity() {
		logx.Warnf("left unverified legacy Codex auth projection: account_id=%s", account.ID)
		return latest, nil
	}
	if legacy.LastRefresh.After(latest.LastRefresh) {
		if err := s.persistManagedCodexRefresh(ctx, account, legacy, legacyPath); err != nil {
			return latest, err
		}
		latest = legacy
	}
	current, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return latest, nil
	}
	if err != nil {
		return latest, managedCodexStorageError(account.ID, err)
	}
	if !os.SameFile(info, current) {
		return latest, status_error.AIAccountCredentialError
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return latest, managedCodexStorageError(account.ID, err)
	}
	return latest, nil
}

func (s *Service) recoverManagedCodexAuth(ctx context.Context, id, home, identity string) error {
	unlock, err := s.credentialGate(id).Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	data, err := readCodexAuthFile(home)
	if errors.Is(err, status_error.RuntimeAuthRequired) {
		return nil
	}
	if err != nil {
		return err
	}
	defer clear(data)
	current, err := decodeCodexChatGPTCredential(data)
	if err != nil || current.Tokens.Identity() != identity {
		// No usable rotation was lost. Release the exited owner so an
		// explicit logout or sign-in can replace the invalid projection.
		logx.Warnf("discarded invalid managed Codex auth refresh: account_id=%s", id)
		return nil
	}
	account, err := s.accounts.Get(ctx, id)
	if err != nil {
		return err
	}
	encoded, err := s.accounts.Credential(ctx, id)
	if err != nil {
		return err
	}
	var saved codexCredential
	if json.Unmarshal([]byte(encoded), &saved) != nil || saved.Tokens.Identity() != identity {
		return status_error.AIAccountCredentialError
	}
	if !current.LastRefresh.After(saved.LastRefresh) {
		return nil
	}
	return s.persistManagedCodexRefresh(ctx, account, current, account.LiveAuthPath)
}

func (s *Service) persistManagedCodexRefresh(ctx context.Context, account models.AIAccount, current codexCredential, path string) error {
	encoded, _ := json.Marshal(current)
	return s.accounts.StoreRefreshedCredential(ctx, account.ID, account.Revision, account.AuthGeneration, string(encoded), path)
}

// RemoveCodexCredentialFiles removes the current native projection before a credential
// mutation commits. The caller must hold the account's exclusive lease.
func (s *Service) RemoveCodexCredentialFiles(ctx context.Context, account models.AIAccount) error {
	if _, ok := accountEndpoint(account, constants.RuntimeIDCodex); !ok || s.dataDirectory == "" {
		return nil
	}
	if account.LiveAuthPath != "" && account.HasCredential && account.AuthMode == constants.AccountAuthModeSubscription {
		secret, err := s.accounts.Credential(ctx, account.ID)
		if err != nil {
			return err
		}
		var saved codexCredential
		if json.Unmarshal([]byte(secret), &saved) == nil && saved.Tokens.Valid() {
			if _, err := s.retireLegacyCodexProjection(ctx, account, saved); err != nil {
				return err
			}
		} else {
			logx.Warnf("left unverified legacy Codex auth projection: account_id=%s", account.ID)
		}
	}
	home, err := s.managedCodexHome(account, false)
	if err != nil {
		return err
	}
	for _, name := range []string{"auth.json", "config.toml"} {
		err := os.Remove(filepath.Join(home, name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return managedCodexStorageError(account.ID, err)
		}
	}
	return nil
}

// SetCodexAuthInvalidator attaches the owner of native connections and their cached facts.
func (s *Service) SetCodexAuthInvalidator(invalidate func(context.Context, string, uint64)) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.invalidateCodexAuth = invalidate
}

// InvalidateCodexAuth fences the previous generation after the mutation has committed.
func (s *Service) InvalidateCodexAuth(ctx context.Context, previous models.AIAccount) {
	if _, ok := accountEndpoint(previous, constants.RuntimeIDCodex); !ok {
		return
	}
	s.mutex.Lock()
	invalidate := s.invalidateCodexAuth
	s.mutex.Unlock()
	if invalidate != nil {
		invalidate(ctx, previous.ID, previous.AuthGeneration)
	}
}

func isManagedCodexID(id string) bool {
	bytes, err := hex.DecodeString(id)
	return err == nil && len(bytes) == 16 && hex.EncodeToString(bytes) == id
}

func (s *Service) managedCodexHome(account models.AIAccount, create bool) (string, error) {
	if !isManagedCodexID(account.ID) || account.AuthGeneration == 0 || !filepath.IsAbs(s.dataDirectory) {
		return "", status_error.AIAccountConfigurationInvalid
	}
	root, err := filepath.EvalSymlinks(s.dataDirectory)
	if err != nil {
		return "", managedCodexStorageError(account.ID, err)
	}
	parts := []string{"ai-accounts", constants.RuntimeIDCodex, account.ID, strconv.FormatUint(account.AuthGeneration, 10)}
	directory := root
	for _, part := range parts {
		directory = filepath.Join(directory, part)
		if create {
			if err := os.Mkdir(directory, 0700); err != nil && !errors.Is(err, os.ErrExist) {
				return "", managedCodexStorageError(account.ID, err)
			}
		}
		info, err := os.Lstat(directory)
		if !create && errors.Is(err, os.ErrNotExist) {
			return filepath.Join(root, filepath.Join(parts...)), nil
		}
		if err != nil {
			return "", managedCodexStorageError(account.ID, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", status_error.AIAccountWorkspaceError
		}
		if create && info.Mode().Perm() != 0700 {
			if err := os.Chmod(directory, 0700); err != nil {
				return "", managedCodexStorageError(account.ID, err)
			}
		}
	}
	return directory, nil
}

func managedCodexStorageError(id string, err error) error {
	logx.Errorf("managed Codex account storage failed: account_id=%s error=%v", id, err)
	return status_error.AIAccountWorkspaceError
}
