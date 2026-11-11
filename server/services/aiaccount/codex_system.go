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

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/codexoauth"
)

// The system selection has no host-managed credential revision. External principal
// switches are represented by a different subject, and token refresh preserves it.
const codexSystemAuthGeneration = 1

// ResolveCodexSystemAccount 冻结原生配置根与可观察的文件账号；缺失登录文件由实际 CLI 判定。
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
	canonical, err := systemAccountHome(configHome, true)
	if err != nil {
		return agent.AccountRequest{}, err
	}

	subject, _, err := readCodexSystemCredential(ctx, canonical)
	if err != nil {
		return agent.AccountRequest{}, err
	}
	request.ConfigHome, request.Subject, request.AuthGeneration = canonical, subject, codexSystemAuthGeneration
	return request, nil
}

// AcquireCodexSystemAccount 核对本地选择；仅已知文件账号要求实际原生进程的凭据隔离证明。
func AcquireCodexSystemAccount(ctx context.Context, request agent.AccountRequest) (agent.AccountLease, error) {
	if request.Provider != constants.RuntimeIDCodex || request.AccountID != agent.SystemAccountID ||
		request.AuthGeneration != codexSystemAuthGeneration || request.Subject == "" || !filepath.IsAbs(request.ConfigHome) {
		return agent.AccountLease{}, status_error.AIAccountConfigurationInvalid
	}
	subject, proof, err := readCodexSystemCredential(ctx, request.ConfigHome)
	if err != nil {
		return agent.AccountLease{}, err
	}
	if subject != request.Subject {
		return agent.AccountLease{}, status_error.RuntimeAuthRequired
	}
	if proof == ([sha256.Size]byte{}) {
		// 无本地登录文件时不要求独立登录回执；只隔离可观察的后续选择变更。
		return agent.AccountLease{ConfigHome: request.ConfigHome, VerifyLogin: func(ctx context.Context) error {
			current, _, err := readCodexSystemCredential(ctx, request.ConfigHome)
			if err != nil {
				return err
			}
			if current != request.Subject {
				return status_error.RuntimeAuthRequired
			}
			return nil
		}}, nil
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
	canonical, err := systemAccountHome(home, true)
	if err != nil || canonical != home {
		return "", noProof, status_error.AIAccountCredentialError
	}
	if _, err := os.Lstat(filepath.Join(home, "auth.json")); errors.Is(err, os.ErrNotExist) {
		return accountSubject("codex-system-unobserved", home), noProof, nil
	} else if err != nil {
		return "", noProof, status_error.AIAccountCredentialError
	}

	credential, err := readCodexChatGPTCredential(home)
	if err != nil {
		return "", noProof, err
	}
	if err := ctx.Err(); err != nil {
		return "", noProof, err
	}
	scope, _ := json.Marshal([]string{"codex-system-chatgpt", home, credential.Tokens.Identity()})
	digest := sha256.Sum256(scope)
	return "codex-system:" + hex.EncodeToString(digest[:]), sha256.Sum256([]byte(credential.Tokens.AccessToken)), nil
}

// readCodexAuthFile reads only the regular file actually selected by the canonical home.
func readCodexAuthFile(home string) ([]byte, error) {
	canonical, err := filepath.EvalSymlinks(home)
	if errors.Is(err, os.ErrNotExist) {
		return nil, status_error.RuntimeAuthRequired
	}
	if err != nil || canonical != home {
		return nil, status_error.AIAccountCredentialError
	}
	return readNativeAccountFile(filepath.Join(home, "auth.json"))
}

func readCodexChatGPTCredential(home string) (codexCredential, error) {
	data, err := readCodexAuthFile(home)
	if err != nil {
		return codexCredential{}, err
	}
	defer clear(data)
	return decodeCodexChatGPTCredential(data)
}

func decodeCodexChatGPTCredential(data []byte) (codexCredential, error) {
	var auth struct {
		// Mode identifies the credential selected by native Codex.
		Mode string `json:"auth_mode"`
		codexLiveAuth
	}
	if json.Unmarshal(data, &auth) != nil {
		return codexCredential{}, status_error.AIAccountCredentialError
	}
	if auth.Mode != "" && auth.Mode != "chatgpt" || auth.APIKey != nil && *auth.APIKey != "" {
		return codexCredential{}, status_error.RuntimeAuthRequired
	}
	tokens := auth.codexLiveAuth.oauthTokens()
	if !tokens.Valid() || tokens.AccountID() != auth.Tokens.AccountID {
		return codexCredential{}, status_error.AIAccountCredentialError
	}
	return codexCredential{Tokens: tokens, LastRefresh: auth.LastRefresh}, nil
}

func (auth codexLiveAuth) oauthTokens() codexoauth.Tokens {
	return codexoauth.Tokens{IDToken: auth.Tokens.IDToken, AccessToken: auth.Tokens.AccessToken, RefreshToken: auth.Tokens.RefreshToken}
}
