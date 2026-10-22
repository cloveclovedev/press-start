package google

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// clientJSON is a client of the type "Desktop app" whose token endpoint is
// tokenURL.
func clientJSON(tokenURL string) string {
	return `{"installed":{"client_id":"id.apps.example","client_secret":"secret",` +
		`"auth_uri":"https://accounts.example/o/oauth2/auth","token_uri":"` + tokenURL + `",` +
		`"redirect_uris":["http://localhost"]}}`
}

// fakeTokenServer answers the code exchange. It fails the test unless the
// request carries the code "the-code" and a PKCE verifier.
func fakeTokenServer(t *testing.T, refreshToken string) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if got := r.PostForm.Get("grant_type"); got != "authorization_code" {
			t.Errorf("grant_type = %q", got)
		}
		if got := r.PostForm.Get("code"); got != "the-code" {
			t.Errorf("code = %q", got)
		}
		if r.PostForm.Get("code_verifier") == "" {
			t.Error("code_verifier is missing")
		}
		if !strings.HasPrefix(r.PostForm.Get("redirect_uri"), "http://127.0.0.1:") {
			t.Errorf("redirect_uri = %q", r.PostForm.Get("redirect_uri"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access", "token_type": "Bearer", "expires_in": 3600,
			"refresh_token": refreshToken,
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return ln
}

// browser returns a showURL that acts as the browser: it sends the redirect
// with the query that query makes from the consent URL.
func browser(t *testing.T, query func(consent url.Values) url.Values) func(string) {
	return func(consentURL string) {
		u, err := url.Parse(consentURL)
		if err != nil {
			t.Errorf("parse consent URL: %v", err)
			return
		}
		redirect := u.Query().Get("redirect_uri")
		go func() {
			resp, err := http.Get(redirect + "?" + query(u.Query()).Encode())
			if err != nil {
				t.Errorf("redirect: %v", err)
				return
			}
			resp.Body.Close()
		}()
	}
}

func TestConfigFromJSONAsksForTheCalendarScopeOnly(t *testing.T) {
	cfg, err := ConfigFromJSON([]byte(clientJSON("https://oauth2.example/token")))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Scopes) != 1 || cfg.Scopes[0] != CalendarScope {
		t.Errorf("Scopes = %v", cfg.Scopes)
	}
	if cfg.ClientID != "id.apps.example" || cfg.Endpoint.TokenURL != "https://oauth2.example/token" {
		t.Errorf("config = %+v", cfg)
	}
}

func TestConfigFromJSONRejectsOtherClientTypes(t *testing.T) {
	for name, data := range map[string]string{
		"web client": `{"web":{"client_id":"id","auth_uri":"a","token_uri":"t"}}`,
		"no id":      `{"installed":{"auth_uri":"a","token_uri":"t"}}`,
		"not JSON":   `client`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ConfigFromJSON([]byte(data)); err == nil {
				t.Error("err = nil")
			}
		})
	}
}

func TestLoadConfigReadsTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	if err := os.WriteFile(path, []byte(clientJSON("https://oauth2.example/token")), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("missing file: err = nil")
	}
}

func TestRefreshTokenConsentURL(t *testing.T) {
	srv, _ := fakeTokenServer(t, "the-refresh-token")
	cfg, err := ConfigFromJSON([]byte(clientJSON(srv.URL)))
	if err != nil {
		t.Fatal(err)
	}
	var consent url.Values
	show := browser(t, func(q url.Values) url.Values {
		consent = q
		return url.Values{"state": {q.Get("state")}, "code": {"the-code"}}
	})
	if _, err := RefreshToken(context.Background(), cfg, listen(t), show); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"scope":                 CalendarScope,
		"code_challenge_method": "S256",
		"access_type":           "offline",
		"prompt":                "consent",
		"response_type":         "code",
	}
	for k, v := range want {
		if got := consent.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if consent.Get("code_challenge") == "" || consent.Get("state") == "" {
		t.Errorf("code_challenge or state is missing: %v", consent)
	}
	if !strings.HasPrefix(consent.Get("redirect_uri"), "http://127.0.0.1:") {
		t.Errorf("redirect_uri = %q", consent.Get("redirect_uri"))
	}
}

func TestRefreshTokenReturnsTheRefreshToken(t *testing.T) {
	srv, calls := fakeTokenServer(t, "the-refresh-token")
	cfg, _ := ConfigFromJSON([]byte(clientJSON(srv.URL)))
	show := browser(t, func(q url.Values) url.Values {
		return url.Values{"state": {q.Get("state")}, "code": {"the-code"}}
	})
	got, err := RefreshToken(context.Background(), cfg, listen(t), show)
	if err != nil {
		t.Fatal(err)
	}
	if got != "the-refresh-token" || *calls != 1 {
		t.Errorf("token = %q, exchanges = %d", got, *calls)
	}
}

func TestRefreshTokenFailsOnABadRedirect(t *testing.T) {
	for name, query := range map[string]func(url.Values) url.Values{
		"wrong state": func(url.Values) url.Values {
			return url.Values{"state": {"other"}, "code": {"the-code"}}
		},
		"missing code": func(q url.Values) url.Values {
			return url.Values{"state": {q.Get("state")}}
		},
		"denied": func(q url.Values) url.Values {
			return url.Values{"state": {q.Get("state")}, "error": {"access_denied"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv, calls := fakeTokenServer(t, "the-refresh-token")
			cfg, _ := ConfigFromJSON([]byte(clientJSON(srv.URL)))
			got, err := RefreshToken(context.Background(), cfg, listen(t), browser(t, query))
			if err == nil || got != "" {
				t.Errorf("token = %q, err = %v", got, err)
			}
			if *calls != 0 {
				t.Errorf("exchanges = %d, want 0", *calls)
			}
		})
	}
}

func TestRefreshTokenFailsWithoutARefreshToken(t *testing.T) {
	srv, _ := fakeTokenServer(t, "")
	cfg, _ := ConfigFromJSON([]byte(clientJSON(srv.URL)))
	show := browser(t, func(q url.Values) url.Values {
		return url.Values{"state": {q.Get("state")}, "code": {"the-code"}}
	})
	if _, err := RefreshToken(context.Background(), cfg, listen(t), show); err == nil {
		t.Error("err = nil")
	}
}

func TestRefreshTokenStopsWithTheContext(t *testing.T) {
	cfg, _ := ConfigFromJSON([]byte(clientJSON("https://oauth2.example/token")))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := RefreshToken(ctx, cfg, listen(t), func(string) {}); err == nil {
		t.Error("err = nil")
	}
}
