package aiaccount

import (
	"context"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func (s *Service) acquireNativeCLIRead(ctx context.Context, request agent.AccountRequest) (func(), error) {
	if !isManagedAccountID(request.AccountID) || !filepath.IsAbs(request.CWD) {
		return nil, status_error.AIAccountConfigurationInvalid
	}
	if err := validateNativeCLIAccountArgs(request.PrefixArgs); err != nil {
		return nil, err
	}
	return s.acquireLease(ctx, request.AccountID, false, false)
}

func (s *Service) resolveManagedNativeCLI(ctx context.Context, request agent.AccountRequest) (agent.AccountRequest, models.AIAccount, models.AIAccountEndpoint, error) {
	account, err := s.accounts.Get(ctx, request.AccountID)
	if err != nil {
		return agent.AccountRequest{}, account, models.AIAccountEndpoint{}, err
	}
	endpoint, ok := accountEndpoint(account, request.Provider)
	if !ok {
		return agent.AccountRequest{}, account, endpoint, status_error.AIAccountConfigurationInvalid
	}
	if !account.HasCredential || account.AuthGeneration == 0 {
		return agent.AccountRequest{}, account, endpoint, status_error.RuntimeAuthRequired
	}
	if err := accountconfig.ValidateEndpoint(endpoint.Protocol, endpoint.BaseURL); err != nil {
		return agent.AccountRequest{}, account, endpoint, err
	}
	switch account.AuthMode {
	case constants.AccountAuthModeAPIKey:
		model := accountconfig.AccountModelID(request.Provider, request.Model)
		if len(account.SelectedModels) == 0 || model != "" && !slices.Contains(account.SelectedModels, model) {
			return agent.AccountRequest{}, account, endpoint, status_error.AIAccountConfigurationInvalid
		}
		request.ConfigHome, err = s.managedAccountHome(request.Provider, account, false, nativeCLIRevisionDirectory(account))
	case constants.AccountAuthModeSubscription:
		// Kimi's existing device login owns this persistent native directory.
		request.ConfigHome, err = s.kimiAccountHome(account.ID, false)
	default:
		err = status_error.AIAccountConfigurationInvalid
	}
	if err != nil {
		return agent.AccountRequest{}, account, endpoint, err
	}
	request.NativeConfigHome, request.DataHome = "", ""
	request.Subject = accountSubject("native-cli-managed-v1", request.Provider, account.ID, string(account.AuthMode))
	request.AuthGeneration = account.AuthGeneration
	request.ConfigFingerprint = accountSubject("native-cli-managed-config-v1",
		strconv.FormatInt(account.Revision, 10), string(endpoint.Protocol), endpoint.BaseURL)
	return request, account, endpoint, nil
}

func (s *Service) acquireManagedNativeCLI(ctx context.Context, request agent.AccountRequest) (agent.AccountLease, error) {
	release, err := s.acquireNativeCLIRead(ctx, request)
	if err != nil {
		return agent.AccountLease{}, err
	}
	lease, err := s.prepareManagedNativeCLI(ctx, request)
	if err != nil {
		release()
		return agent.AccountLease{}, err
	}
	lease.Close = func(context.Context) error { release(); return nil }
	return lease, nil
}

func (s *Service) prepareManagedNativeCLI(ctx context.Context, request agent.AccountRequest) (agent.AccountLease, error) {
	unlock, err := s.credentialGate(request.AccountID).Lock(ctx)
	if err != nil {
		return agent.AccountLease{}, err
	}
	defer unlock()
	resolved, account, endpoint, err := s.resolveManagedNativeCLI(ctx, request)
	if err != nil {
		return agent.AccountLease{}, err
	}
	if resolved.Subject != request.Subject || resolved.AuthGeneration != request.AuthGeneration {
		return agent.AccountLease{}, status_error.RuntimeAuthRequired
	}
	if !resolved.Equal(request) {
		return agent.AccountLease{}, status_error.RuntimeConfigurationInvalid
	}
	secret, err := s.accounts.Credential(ctx, account.ID)
	if err != nil {
		return agent.AccountLease{}, err
	}
	if strings.TrimSpace(secret) == "" {
		return agent.AccountLease{}, status_error.RuntimeAuthRequired
	}
	if account.AuthMode == constants.AccountAuthModeSubscription {
		if secret != kimiCredentialMarker {
			return agent.AccountLease{}, status_error.AIAccountCredentialError
		}
		return agent.AccountLease{ConfigHome: request.ConfigHome,
			Environment: accountconfig.HomeEnvironment(request.Provider, request.ConfigHome)}, nil
	}
	home, err := s.managedAccountHome(request.Provider, account, true, nativeCLIRevisionDirectory(account))
	if err != nil {
		return agent.AccountLease{}, err
	}
	if home != request.ConfigHome {
		return agent.AccountLease{}, status_error.RuntimeConfigurationInvalid
	}
	model := account.DefaultModel
	if !slices.Contains(account.SelectedModels, model) {
		model = account.SelectedModels[0]
	}
	environment, err := accountconfig.PrepareForWorkspace(home, request.CWD, accountconfig.Spec{
		ID: account.ID, RuntimeID: request.Provider, AuthMode: account.AuthMode, Protocol: endpoint.Protocol, BaseURL: endpoint.BaseURL,
		APIKey: secret, Model: model, MaxContextSize: account.ModelContextSizes[model], Revision: account.Revision,
		SelectedModels: account.SelectedModels, ModelContextSizes: account.ModelContextSizes,
		ModelDisplayNames: account.ModelDisplayNames, ModelReasoningLevels: account.ModelReasoningLevels,
	})
	if err != nil {
		return agent.AccountLease{}, err
	}
	return agent.AccountLease{ConfigHome: home, Environment: environment}, nil
}

func nativeCLIRevisionDirectory(account models.AIAccount) string {
	return "revision-" + strconv.FormatInt(account.Revision, 10)
}
