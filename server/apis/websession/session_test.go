package websession

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/localweb"
	"github.com/gin-gonic/gin"
)

func TestCreateSessionWithoutPairingCodeAndReuseCookie(t *testing.T) {
	manager, err := localweb.New("127.0.0.1:8432", "", "http://127.0.0.1:1422")
	if err != nil {
		t.Fatal(err)
	}
	previous := global.LocalWeb
	global.LocalWeb = manager
	t.Cleanup(func() { global.LocalWeb = previous; manager.Close() })
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8432/api/web/session", nil)
	ctx.Request.Header.Set("Origin", "http://127.0.0.1:1422")
	value, err := (&CreateSession{}).Output(ctx)
	if err != nil || ctx.IsAborted() {
		t.Fatalf("create without code: %d %v", w.Code, err)
	}
	created := value.(SessionResponse)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || created.CSRFToken == "" || !cookies[0].HttpOnly || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing session cookie, CSRF or cache control")
	}
	read := httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(read)
	ctx.Request = httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8432/api/web/session", nil)
	ctx.Request.AddCookie(cookies[0])
	value, err = (&GetSession{}).Output(ctx)
	if err != nil || ctx.IsAborted() || value.(SessionResponse).CSRFToken != created.CSRFToken || len(read.Result().Cookies()) != 0 {
		t.Fatal("refresh did not reuse existing session")
	}
}
