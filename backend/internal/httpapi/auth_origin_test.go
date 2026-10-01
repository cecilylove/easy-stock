package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"easy-stock/backend/internal/appsettings"
	"github.com/gorilla/websocket"
)

func TestBrowserOriginRejectsCrossSiteMutationAndPreflight(t *testing.T) {
	store, err := appsettings.Open("")
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(Config{SettingsStore: store, EnforceLoopbackHost: true})
	defer s.Close()
	original := store.Snapshot().LLM.Model
	for _, method := range []string{http.MethodOptions, http.MethodPut} {
		req := httptest.NewRequest(method, "http://127.0.0.1:20081/api/v1/settings", strings.NewReader(`{"llm":{"model":"unexpected"}}`))
		req.Header.Set("Origin", "https://untrusted.example")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Access-Control-Request-Method", "PUT")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden || rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("%s untrusted request: status=%d CORS=%q", method, rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
		}
	}
	if store.Snapshot().LLM.Model != original {
		t.Fatal("untrusted browser changed settings")
	}

	req := httptest.NewRequest(http.MethodPut, "http://127.0.0.1:20081/api/v1/settings", strings.NewReader(`{"llm":{"model":"trusted-local-change"}}`))
	req.Header.Set("Origin", "http://127.0.0.1:20073")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || store.Snapshot().LLM.Model != "trusted-local-change" {
		t.Fatalf("trusted development frontend rejected: %d %s", rec.Code, rec.Body.String())
	}
}

func TestBrowserOriginHostAndDesktopBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, host, origin, token, bearer, fetchSite string
		allowed                                      []string
		want                                         int
	}{
		{name: "CLI loopback", host: "127.0.0.1:20081", want: 200},
		{name: "IPv6 frontend", host: "[::1]:20081", origin: "http://[::1]:20073", want: 200},
		{name: "configured local frontend", host: "localhost:20081", origin: "http://localhost:4173", allowed: []string{"http://localhost:4173"}, want: 200},
		{name: "unconfigured port", host: "localhost:20081", origin: "http://localhost:4444", want: 403},
		{name: "opaque Web origin", host: "127.0.0.1:20081", origin: "null", want: 403},
		{name: "desktop with token", host: "127.0.0.1:20000", origin: "null", token: "secret", bearer: "secret", want: 200},
		{name: "desktop without token", host: "127.0.0.1:20000", origin: "null", token: "secret", want: 401},
		{name: "external even with token", host: "127.0.0.1:20000", origin: "https://untrusted.example", token: "secret", bearer: "secret", want: 403},
		{name: "rebind Host", host: "untrusted.example:20081", want: 403},
		{name: "rebind with legitimate Origin", host: "untrusted.example:20081", origin: "http://127.0.0.1:20073", want: 403},
		{name: "originless cross-site fetch", host: "127.0.0.1:20081", fetchSite: "cross-site", want: 403},
		{name: "loopback-looking Origin", host: "127.0.0.1:20081", origin: "http://localhost.untrusted.example:20073", want: 403},
		{name: "userinfo disguise", host: "127.0.0.1:20081", origin: "http://localhost:20073@untrusted.example", want: 403},
		{name: "remote allowlist rejected", host: "127.0.0.1:20081", origin: "https://untrusted.example", allowed: []string{"https://untrusted.example"}, want: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewServer(Config{Token: tc.token, AllowedOrigins: tc.allowed, EnforceLoopbackHost: true})
			defer s.Close()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+tc.bearer)
			}
			req.Header.Set("Sec-Fetch-Site", tc.fetchSite)
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestWebSocketRejectsUntrustedOriginBeforeUpgrade(t *testing.T) {
	s := NewServer(Config{Realtime: fakeRealtimeProvider{}, EnforceLoopbackHost: true})
	defer s.Close()
	ts := httptest.NewServer(s)
	defer ts.Close()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/ws/stream?symbols=000001.SZ&interval_ms=500"
	for _, origin := range []string{"https://untrusted.example", "null"} {
		_, response, err := websocket.DefaultDialer.Dial(url, http.Header{"Origin": []string{origin}})
		if response != nil {
			defer response.Body.Close()
		}
		if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
			t.Fatalf("untrusted WebSocket origin %q: response=%v err=%v", origin, response, err)
		}
	}
}
