package automatic_tasks

import (
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

func callLocal(s *taskAPI, method, url, token, key, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:8431"+url, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	s.localHandler(constants.AutomaticTaskDefaultAPIIP).ServeHTTP(w, r)
	return w
}

func TestAutomaticLocalAPIContractAndSecurity(t *testing.T) {
	h := newHarness(t)
	saved := h.save(t, constants.AutomaticTaskTriggerAPI)
	url := "/v1/automatic-tasks/" + saved.TaskID + "/runs"
	for _, body := range []string{"null", "[]", "{", `{"runtimeId":"override"}`, `{"input":1}`, `{} {}`, `{"input":"` + strings.Repeat("中", 4001) + `"}`, `{"input":"` + strings.Repeat("a", 65536) + `"}`} {
		if w := callLocal(&h.api, "POST", url, saved.Token, "invalid", body); w.Code != 400 {
			t.Fatalf("invalid body accepted: %d", w.Code)
		}
	}
	if w := callLocal(&h.api, "POST", url, "wrong", "test", `{}`); w.Code != 401 {
		t.Fatal("invalid task token accepted")
	}
	if w := callLocal(&h.api, "POST", url, saved.Token, "", `{}`); w.Code != 400 {
		t.Fatal("missing key accepted")
	}
	for _, host := range []string{"evil.test:8431", "127.0.0.1.evil.test:8431", "[::1]:8431"} {
		r := httptest.NewRequest("POST", "http://127.0.0.1:8431"+url, strings.NewReader(`{}`))
		r.Host = host
		r.Header.Set("Authorization", "Bearer "+saved.Token)
		w := httptest.NewRecorder()
		h.api.localHandler(constants.AutomaticTaskDefaultAPIIP).ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("untrusted host accepted")
		}
	}
	r := httptest.NewRequest("POST", "http://127.0.0.1:8431"+url, strings.NewReader(`{}`))
	r.Header.Set("Origin", "http://127.0.0.1:1420")
	w := httptest.NewRecorder()
	h.api.localHandler(constants.AutomaticTaskDefaultAPIIP).ServeHTTP(w, r)
	if w.Code != 401 || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("browser cross-origin request accepted")
	}
	w = callLocal(&h.api, "POST", url, saved.Token, "accepted", `{"input":"private text"}`)
	if w.Code != 202 {
		t.Fatalf("POST: %d %s", w.Code, w.Body.String())
	}
	var receipt PublicRun
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil || receipt.RunID == "" {
		t.Fatal("missing durable receipt")
	}
	if strings.Contains(w.Body.String(), "private text") || strings.Contains(w.Body.String(), "instruction") || strings.Contains(w.Body.String(), "chatId") {
		t.Fatal("trigger leaked management content")
	}
	if w := callLocal(&h.api, "POST", url, saved.Token, "accepted", `{"input":"private text"}`); w.Code != 202 {
		t.Fatal("accepted request cannot replay")
	}
	if w := callLocal(&h.api, "POST", url, saved.Token, "accepted", `{"input":"different"}`); w.Code != 409 {
		t.Fatal("same key different input accepted")
	}
	w = callLocal(&h.api, "GET", url+"/"+receipt.RunID, saved.Token, "", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "configuration") {
		t.Fatal("limited state read failed")
	}
	other := h.save(t, constants.AutomaticTaskTriggerAPI)
	if w := callLocal(&h.api, "GET", "/v1/automatic-tasks/"+other.TaskID+"/runs/"+receipt.RunID, other.Token, "", ""); w.Code != 404 {
		t.Fatal("cross-task state read accepted")
	}
}

func TestAutomaticAPIRateCountsAcceptedOnly(t *testing.T) {
	h := newHarness(t)
	saved := h.save(t, constants.AutomaticTaskTriggerAPI)
	ctx := context.Background()
	for i := range constants.AutomaticTaskAPIRequestsPerMinute {
		run, err := h.api.TriggerAPI(ctx, saved.TaskID, saved.Token, strconv.Itoa(i), "")
		if err != nil {
			t.Fatal(err)
		}
		h.boundary.finish(run.ChatID, constants.ExecutionStatusCompleted, false)
	}
	if _, err := h.api.TriggerAPI(ctx, saved.TaskID, saved.Token, "limited", ""); util.ErrorCode(err, status_error.AutomaticTaskServiceInvalid.Key()) != "AutomaticTaskRateLimited" {
		t.Fatal("rate limit was not enforced")
	}
	if _, err := h.api.TriggerAPI(ctx, saved.TaskID, saved.Token, "0", ""); err != nil {
		t.Fatal("rate limit blocked existing receipt")
	}
	h.now.Add(60_001)
	if _, err := h.api.TriggerAPI(ctx, saved.TaskID, saved.Token, "limited", ""); err != nil {
		t.Fatal("rejected key was consumed")
	}
}

func availableAPIPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

func TestAutomaticAPIHostMatchesConfiguredAddress(t *testing.T) {
	for _, value := range []struct {
		// bind 是已配置的监听地址。
		bind string
		// host 是 HTTP 请求声明的目标。
		host string
		// allowed 表示该目标与监听设置一致。
		allowed bool
	}{
		{"127.0.0.1", "localhost:8431", true},
		{"127.0.0.1", "[::1]:8431", false},
		{"192.168.1.20", "192.168.1.20:8431", true},
		{"192.168.1.20", "192.168.1.21:8431", false},
		{"192.168.1.20", "evil.test:8431", false},
		{"0.0.0.0", "192.168.1.20:8431", true},
		{"0.0.0.0", "127.0.0.1.evil.test:8431", false},
		{"0.0.0.0", "[::1]:8431", false},
		{"::1", "[::1]:8431", true},
		{"::", "[2001:db8::1]:8431", true},
	} {
		if got := apiHostAllowed(value.host, net.ParseIP(value.bind)); got != value.allowed {
			t.Errorf("bind=%s host=%s got=%v", value.bind, value.host, got)
		}
	}
}
