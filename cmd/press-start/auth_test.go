package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeClient writes an OAuth client file of the type "Desktop app" whose
// token endpoint is a fake that returns the refresh token "the-refresh-token".
func writeClient(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"access","token_type":"Bearer","expires_in":3600,"refresh_token":"the-refresh-token"}`))
	}))
	t.Cleanup(srv.Close)
	path := filepath.Join(t.TempDir(), "client.json")
	data := `{"installed":{"client_id":"id","client_secret":"secret","auth_uri":"https://accounts.example/auth","token_uri":"` + srv.URL + `"}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// redirectWith acts as the browser: it sends the redirect with the query that
// query makes from the consent URL.
func redirectWith(t *testing.T, query func(consent url.Values) url.Values) func(string) {
	return func(consentURL string) {
		u, err := url.Parse(consentURL)
		if err != nil {
			t.Errorf("parse consent URL: %v", err)
			return
		}
		go func() {
			resp, err := http.Get(u.Query().Get("redirect_uri") + "?" + query(u.Query()).Encode())
			if err != nil {
				t.Errorf("redirect: %v", err)
				return
			}
			resp.Body.Close()
		}()
	}
}

// filesIn lists the names of the files in dir.
func filesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestAuthPrintsTheRefreshTokenOnceAndWritesNoFile(t *testing.T) {
	client := writeClient(t)
	work := t.TempDir()
	t.Chdir(work)
	var stdout, stderr bytes.Buffer
	show := redirectWith(t, func(q url.Values) url.Values {
		return url.Values{"state": {q.Get("state")}, "code": {"the-code"}}
	})
	if err := runAuthWith(context.Background(), []string{"-client", client}, &stdout, &stderr, show); err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); got != "the-refresh-token\n" {
		t.Errorf("stdout = %q", got)
	}
	if strings.Contains(stderr.String(), "the-refresh-token") {
		t.Errorf("stderr holds the token: %q", stderr.String())
	}
	if got := filesIn(t, work); len(got) != 0 {
		t.Errorf("files in the work directory: %v", got)
	}
	if got := filesIn(t, filepath.Dir(client)); len(got) != 1 {
		t.Errorf("files beside the client: %v", got)
	}
}

func TestAuthFailsOnABadRedirectAndPrintsNoToken(t *testing.T) {
	for name, query := range map[string]func(url.Values) url.Values{
		"wrong state": func(url.Values) url.Values {
			return url.Values{"state": {"other"}, "code": {"the-code"}}
		},
		"missing code": func(q url.Values) url.Values {
			return url.Values{"state": {q.Get("state")}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := runAuthWith(context.Background(), []string{"-client", writeClient(t)}, &stdout, &stderr, redirectWith(t, query))
			if err == nil {
				t.Error("err = nil")
			}
			if stdout.Len() != 0 || strings.Contains(stderr.String(), "the-refresh-token") {
				t.Errorf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestAuthNeedsTheClientFile(t *testing.T) {
	for name, args := range map[string][]string{
		"no flag":      nil,
		"missing file": {"-client", filepath.Join(t.TempDir(), "missing.json")},
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := runAuthWith(context.Background(), args, &stdout, &stderr, func(string) {}); err == nil {
				t.Error("err = nil")
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q", stdout.String())
			}
		})
	}
}
