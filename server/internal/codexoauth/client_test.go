package codexoauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	ginxlogx "github.com/shrewx/ginx/pkg/logx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

func testToken(subject string) string {
	data, _ := json.Marshal(map[string]any{"sub": subject, "exp": time.Now().Add(time.Hour).Unix(), "https://api.openai.com/auth": map[string]string{"chatgpt_account_id": "workspace"}})
	return "header." + base64.RawURLEncoding.EncodeToString(data) + ".signature"
}

func TestDeviceFlowUsesDeviceRequestsAndPendingSemantics(t *testing.T) {
	polls := 0
	token := testToken("user")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["client_id"] != constants.CodexOAuthClientID {
				t.Error("wrong OAuth client")
			}
			_, _ = w.Write([]byte(`{"device_auth_id":"device-secret","user_code":"USER-CODE","interval":"5"}`))
		case "/api/accounts/deviceauth/token":
			polls++
			if polls == 1 {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte(`{"authorization_code":"authorization","code_verifier":"verifier"}`))
		case "/oauth/token":
			_ = r.ParseForm()
			if r.Form.Get("code_verifier") != "verifier" || r.Form.Get("redirect_uri") != constants.CodexOAuthRedirectURI {
				t.Error("wrong device exchange")
			}
			_ = json.NewEncoder(w).Encode(Tokens{AccessToken: token, RefreshToken: "refresh", IDToken: token, ExpiresIn: 3600})
		default:
			t.Errorf("unexpected URL: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), Origin: server.URL}
	device, err := client.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if device.ExpiresIn != 900 || device.PollInterval() != 8*time.Second {
		t.Fatal("device defaults differ from reference")
	}
	pending, err := client.Poll(context.Background(), device)
	if err != nil || pending != nil {
		t.Fatalf("403 must stay pending: %v", err)
	}
	tokens, err := client.Poll(context.Background(), device)
	if err != nil {
		t.Fatal(err)
	}
	if !tokens.Valid() || tokens.Identity() != "user/workspace" {
		t.Fatal("complete token bundle not preserved")
	}
}

func TestRefreshPreservesUnrotatedFieldsAndRejectsIdentityChange(t *testing.T) {
	token := testToken("user")
	previous := Tokens{AccessToken: token, RefreshToken: "refresh", IDToken: token}
	changeIdentity := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("scope") != "openid profile email" || r.Form.Get("refresh_token") != "refresh" {
			t.Error("wrong refresh request")
		}
		result := Tokens{AccessToken: token}
		if changeIdentity {
			result.IDToken = testToken("other-user")
		}
		_ = json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), Origin: server.URL}
	result, err := client.Refresh(context.Background(), previous)
	if err != nil {
		t.Fatal(err)
	}
	if result.RefreshToken != previous.RefreshToken || result.IDToken != previous.IDToken {
		t.Fatal("missing fields erased refresh identity")
	}
	changeIdentity = true
	if _, err := client.Refresh(context.Background(), previous); err == nil {
		t.Fatal("refresh accepted a different user")
	}
}

func TestRequestFailuresAreDiagnosableWithoutSecrets(t *testing.T) {
	logger := ginxlogx.Instance()
	previous := logger.Out
	var output bytes.Buffer
	logger.SetOutput(&output)
	t.Cleanup(func() { logger.SetOutput(previous) })
	const secret = "private-token-must-not-appear"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/invalid" {
			_, _ = w.Write([]byte(secret))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(secret))
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), Origin: server.URL}
	var result Tokens
	_, err := client.request(context.Background(), "/oauth/token", nil, nil, &result)
	if !errors.Is(err, status_error.AIAccountLoginFailed) || !strings.Contains(output.String(), "status_code=503") {
		t.Fatalf("HTTP failure lacks safe diagnostic: %v %s", err, output.String())
	}
	output.Reset()
	_, err = client.request(context.Background(), "/invalid", nil, nil, &result)
	if !errors.Is(err, status_error.AIAccountLoginFailed) || !strings.Contains(output.String(), "stage=decode") || strings.Contains(output.String(), secret) {
		t.Fatalf("decode failure diagnostic is missing or exposes response: %v %s", err, output.String())
	}
	server.Close()
	output.Reset()
	_, err = client.request(context.Background(), "/oauth/token", nil, nil, &result)
	if !errors.Is(err, status_error.AIAccountLoginFailed) || !strings.Contains(output.String(), "stage=transport") {
		t.Fatalf("transport failure lacks diagnostic: %v %s", err, output.String())
	}
}
