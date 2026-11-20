package oauth

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

const (
	callbackPath       = "/mcp/oauth/callback"
	waitForGrant       = 10 * time.Minute
	waitForLocalCommit = 15 * time.Second
	callbackSuccess    = "Account connected in NeuxBot. You can close this tab."
	callbackFailure    = "NeuxBot could not complete authorization. Return to NeuxBot to try again."
	callbackUnknown    = "Authorization has no confirmed result. Return to NeuxBot to check its status."
)

type callbackOutcome uint8

const (
	callbackUnconfirmed callbackOutcome = iota
	callbackRejected
	callbackConnected
)

// grant 是已由本机监听器核验并认领的一次授权回调。
type grant struct {
	// code 仅交付原授权请求的令牌交换。
	code string
	// state 绑定单次等待会话。
	state string
	// issuer 是授权服务器返回的 RFC 9207 签发方。
	issuer string
	// denied 表示浏览器返回了拒绝或取消。
	denied bool
	// finished 只向本次通过校验的浏览器请求提供最终授权结果。
	finished chan callbackOutcome
}

// Authorize 在短时 IPv4 回环监听器上等待一次回调。交换成功后调用方须回报本机授权提交结果。
func (c *Client) Authorize(ctx context.Context, scopes []string, present func(string) error) (Credential, func(bool), error) {
	if present == nil {
		return Credential{}, nil, status_error.PluginNotReady
	}
	ctx, cancel := context.WithTimeout(ctx, waitForGrant)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		cancel()
		logx.Errorf("start MCP OAuth callback listener failed: error=%v", err)
		return Credential{}, nil, status_error.PluginAuthorizationFailed
	}
	host := listener.Addr().String()
	attempt, err := c.Begin(ctx, "http://"+host+callbackPath, scopes)
	if err != nil {
		cancel()
		_ = listener.Close()
		return Credential{}, nil, err
	}
	received := make(chan grant, 1)
	server := &http.Server{
		Handler:           callbackHandler(ctx, attempt, host, received),
		ReadTimeout:       3 * time.Second,
		ReadHeaderTimeout: 3 * time.Second,
		WriteTimeout:      protocolTimeout + waitForLocalCommit + 5*time.Second,
		MaxHeaderBytes:    8 << 10,
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	closeServer := func() {
		defer cancel()
		shutdown, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
		}
	}
	awaitingCommit := false
	defer func() {
		if !awaitingCommit {
			closeServer()
		}
	}()

	if err := present(attempt.URL); err != nil {
		cancel()
		return Credential{}, nil, err
	}
	select {
	case result := <-received:
		if ctx.Err() != nil {
			return Credential{}, nil, ctx.Err()
		}
		if result.denied {
			result.finished <- callbackRejected
			return Credential{}, nil, status_error.PluginAuthorizationFailed
		}
		credential, err := c.Complete(ctx, attempt, result.code, result.state, result.issuer)
		if err != nil {
			result.finished <- callbackRejected
			return Credential{}, nil, err
		}
		awaitingCommit = true
		confirmation, stop := context.WithTimeout(ctx, waitForLocalCommit)
		var once sync.Once
		reply := func(outcome callbackOutcome) {
			once.Do(func() {
				result.finished <- outcome
				stop()
				closeServer()
			})
		}
		context.AfterFunc(confirmation, func() { reply(callbackUnconfirmed) })
		finish := func(connected bool) {
			if connected {
				reply(callbackConnected)
			} else {
				reply(callbackRejected)
			}
		}
		return credential, finish, nil
	case <-ctx.Done():
		return Credential{}, nil, ctx.Err()
	case err := <-serveDone:
		if !errors.Is(err, http.ErrServerClosed) {
			logx.Errorf("serve MCP OAuth callback failed: error=%v", err)
		}
		return Credential{}, nil, status_error.PluginAuthorizationFailed
	}
}

func callbackHandler(ctx context.Context, attempt *Attempt, host string, received chan<- grant) http.Handler {
	var accepted atomic.Bool
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if request.Method != http.MethodGet || request.Host != host || request.URL.EscapedPath() != callbackPath ||
			request.ContentLength != 0 || len(request.TransferEncoding) != 0 ||
			len(request.URL.RawQuery) > 8<<10 || ctx.Err() != nil {
			http.Error(w, "Invalid NeuxBot authorization response.", http.StatusBadRequest)
			return
		}
		params, err := url.ParseQuery(request.URL.RawQuery)
		code, state, issuer, denial := params.Get("code"), params.Get("state"), params.Get("iss"), params.Get("error")
		if err != nil || len(params["state"]) != 1 || len(params["code"]) > 1 ||
			len(params["iss"]) > 1 || len(params["error"]) > 1 || (code == "") == (denial == "") ||
			!attempt.matchesCallback(state, issuer) || !accepted.CompareAndSwap(false, true) {
			http.Error(w, "Invalid NeuxBot authorization response.", http.StatusBadRequest)
			return
		}
		finished := make(chan callbackOutcome, 1)
		received <- grant{code: code, state: state, issuer: issuer, denied: denial != "", finished: finished}
		select {
		case outcome := <-finished:
			switch outcome {
			case callbackConnected:
				_, _ = io.WriteString(w, callbackSuccess)
			case callbackRejected:
				http.Error(w, callbackFailure, http.StatusBadRequest)
			default:
				http.Error(w, callbackUnknown, http.StatusAccepted)
			}
		case <-ctx.Done():
			http.Error(w, callbackUnknown, http.StatusAccepted)
		case <-request.Context().Done():
		}
	})
}

func (a *Attempt) matchesCallback(state, issuer string) bool {
	return subtle.ConstantTimeCompare([]byte(a.State), []byte(state)) == 1 &&
		(issuer == a.binding.Issuer || (issuer == "" && !a.requireIssuer))
}
