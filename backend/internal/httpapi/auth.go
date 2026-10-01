package httpapi

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Restrict browsers before CORS/preflight or WebSocket upgrade. CLI requests
// need no Origin; the executable also enables the loopback Host guard to reject
// DNS-rebinding hosts. Electron's opaque file origin still requires its token.
func (s *Server) browserRequestAllowed(r *http.Request) bool {
	if s.enforceLoopbackHost && !loopbackHost(r.Host) {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return r.Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	if origin == "null" {
		return s.token != ""
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || !loopbackHost(u.Host) {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	for _, allowed := range append([]string{"http://127.0.0.1:20073", "http://localhost:20073", "http://[::1]:20073"}, s.allowedOrigins...) {
		if strings.EqualFold(origin, strings.TrimRight(strings.TrimSpace(allowed), "/")) {
			return true
		}
	}
	return false
}

func loopbackHost(host string) bool {
	u, err := url.Parse("http://" + host)
	if err != nil || u.Host != host || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	name := u.Hostname()
	if strings.EqualFold(name, "localhost") {
		return true
	}
	ip := net.ParseIP(name)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) withCORS(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
	}
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-A-Stock-Token, X-Request-ID")
	w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
}

func (s *Server) authorized(r *http.Request) bool {
	if s.token == "" || r.URL.Path == "/api/health" || r.Method == http.MethodOptions {
		return true
	}
	if r.URL.Query().Get("token") == s.token {
		return true
	}
	if r.Header.Get("X-A-Stock-Token") == s.token {
		return true
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(auth, "Bearer ") && strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")) == s.token {
		return true
	}
	return false
}
