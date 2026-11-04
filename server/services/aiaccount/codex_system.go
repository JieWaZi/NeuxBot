package aiaccount

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
	"github.com/JieWaZi/NeuxBot/server/internal/codexoauth"
)

// The system selection has no host-managed credential revision. External principal
// switches are represented by a different subject, and token refresh preserves it.
const codexSystemAuthGeneration = 1

// ResolveCodexSystemAccount freezes the selected native configuration root and file-backed ChatGPT principal.
func ResolveCodexSystemAccount(ctx context.Context, request agent.AccountRequest, configHome string) (agent.AccountRequest, error) {
	if request.Provider != constants.RuntimeIDCodex || request.AccountID != agent.SystemAccountID {
		return agent.AccountRequest{}, status_error.AIAccountConfigurationInvalid
	}
	if err := ctx.Err(); err != nil {
		return agent.AccountRequest{}, err
	}
	if configHome == "" {
		configHome = os.Getenv("CODEX_HOME")
	}
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return agent.AccountRequest{}, status_error.RuntimeConfigurationInvalid
		}
		configHome = filepath.Join(home, ".codex")
	}
	if !filepath.IsAbs(configHome) {
		return agent.AccountRequest{}, status_error.RuntimeConfigurationInvalid
	}
	canonical, err := filepath.EvalSymlinks(configHome)
	if errors.Is(err, os.ErrNotExist) {
		return agent.AccountRequest{}, status_error.RuntimeAuthRequired
	}
	if err != nil {
		return agent.AccountRequest{}, status_error.AIAccountCredentialError
	}
	subject, _, err := readCodexSystemCredential(ctx, canonical)
	if err != nil {
		return agent.AccountRequest{}, err
	}
	request.ConfigHome, request.Subject, request.AuthGeneration = canonical, subject, codexSystemAuthGeneration
	return request, nil
}

// AcquireCodexSystemAccount revalidates admission and requires proof from the initialized process.
func AcquireCodexSystemAccount(ctx context.Context, request agent.AccountRequest) (agent.AccountLease, error) {
	if request.Provider != constants.RuntimeIDCodex || request.AccountID != agent.SystemAccountID ||
		request.AuthGeneration != codexSystemAuthGeneration || request.Subject == "" || !filepath.IsAbs(request.ConfigHome) {
		return agent.AccountLease{}, status_error.AIAccountConfigurationInvalid
	}
	subject, _, err := readCodexSystemCredential(ctx, request.ConfigHome)
	if err != nil {
		return agent.AccountLease{}, err
	}
	if subject != request.Subject {
		return agent.AccountLease{}, status_error.RuntimeAuthRequired
	}
	return agent.AccountLease{
		ConfigHome: request.ConfigHome,
		VerifyCodexCredential: func(ctx context.Context, active [sha256.Size]byte) error {
			subject, selected, err := readCodexSystemCredential(ctx, request.ConfigHome)
			if err != nil {
				return err
			}
			if subject != request.Subject || subtle.ConstantTimeCompare(active[:], selected[:]) != 1 {
				return status_error.RuntimeAuthRequired
			}
			return nil
		},
	}, nil
}

func readCodexSystemCredential(ctx context.Context, home string) (string, [sha256.Size]byte, error) {
	var noProof [sha256.Size]byte
	if err := ctx.Err(); err != nil {
		return "", noProof, err
	}
	canonical, err := filepath.EvalSymlinks(home)
	if errors.Is(err, os.ErrNotExist) {
		return "", noProof, status_error.RuntimeAuthRequired
	}
	if err != nil || canonical != home {
		return "", noProof, status_error.AIAccountCredentialError
	}
	path := filepath.Join(home, "auth.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", noProof, status_error.RuntimeAuthRequired
	}
	const maxAuthBytes = 1 << 20
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxAuthBytes {
		return "", noProof, status_error.AIAccountCredentialError
	}
	file, err := os.Open(path)
	if err != nil {
		return "", noProof, status_error.AIAccountCredentialError
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return "", noProof, status_error.AIAccountCredentialError
	}
	data, err := io.ReadAll(io.LimitReader(file, maxAuthBytes+1))
	if err != nil || len(data) > maxAuthBytes {
		return "", noProof, status_error.AIAccountCredentialError
	}
	defer clear(data)
	var auth struct {
		// Mode identifies the credential selected by native Codex.
		Mode string `json:"auth_mode"`
		codexLiveAuth
	}
	if json.Unmarshal(data, &auth) != nil {
		return "", noProof, status_error.AIAccountCredentialError
	}
	if auth.Mode != "" && auth.Mode != "chatgpt" || auth.APIKey != nil && *auth.APIKey != "" {
		return "", noProof, status_error.RuntimeAuthRequired
	}
	tokens := codexoauth.Tokens{IDToken: auth.Tokens.IDToken, AccessToken: auth.Tokens.AccessToken, RefreshToken: auth.Tokens.RefreshToken}
	if !tokens.Valid() || tokens.AccountID() != auth.Tokens.AccountID {
		return "", noProof, status_error.AIAccountCredentialError
	}
	if err := ctx.Err(); err != nil {
		return "", noProof, err
	}
	scope, _ := json.Marshal([]string{"codex-system-chatgpt-v1", home, tokens.Identity()})
	digest := sha256.Sum256(scope)
	return "codex-system:" + hex.EncodeToString(digest[:]), sha256.Sum256([]byte(tokens.AccessToken)), nil
}
