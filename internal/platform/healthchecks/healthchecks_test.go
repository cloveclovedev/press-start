package healthchecks

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type request struct {
	method      string
	path        string
	contentType string
	body        string
}

// fakeServer records the first request that the test has not read yet, and
// answers every request with status.
func fakeServer(t *testing.T, status int) (*httptest.Server, <-chan request) {
	t.Helper()
	got := make(chan request, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		select {
		case got <- request{r.Method, r.URL.Path, r.Header.Get("Content-Type"), string(body)}:
		default:
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

// The ping URL of a check ends with the key of the check.
const key = "/0f7a1b2c-secret-key"

func TestSuccessSendsGetToThePingURL(t *testing.T) {
	srv, got := fakeServer(t, http.StatusOK)
	if err := New(srv.URL + key).Success(context.Background()); err != nil {
		t.Fatalf("Success: %v", err)
	}
	r := <-got
	if r.method != http.MethodGet || r.path != key {
		t.Errorf("request = %s %s, want GET %s", r.method, r.path, key)
	}
}

func TestFailSendsPostWithTheBody(t *testing.T) {
	srv, got := fakeServer(t, http.StatusOK)
	if err := New(srv.URL+key).Fail(context.Background(), "alarm did not ring"); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	r := <-got
	if r.method != http.MethodPost || r.path != key+"/fail" {
		t.Errorf("request = %s %s, want POST %s/fail", r.method, r.path, key)
	}
	if !strings.HasPrefix(r.contentType, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", r.contentType)
	}
	if r.body != "alarm did not ring" {
		t.Errorf("body = %q, want %q", r.body, "alarm did not ring")
	}
}

func TestServerErrorIsAnErrorWithoutTheURL(t *testing.T) {
	srv, _ := fakeServer(t, http.StatusServiceUnavailable)
	c := New(srv.URL + key)
	for name, call := range map[string]func() error{
		"Success": func() error { return c.Success(context.Background()) },
		"Fail":    func() error { return c.Fail(context.Background(), "x") },
	} {
		err := call()
		if err == nil {
			t.Fatalf("%s: got nil error for status 503", name)
		}
		if strings.Contains(err.Error(), key) || strings.Contains(err.Error(), srv.URL) {
			t.Errorf("%s: error %q holds the ping URL", name, err)
		}
	}
}

func TestServerThatNeverAnswersTimesOut(t *testing.T) {
	stop := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-stop
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(stop) })

	c := New(srv.URL + key)
	c.timeout = 50 * time.Millisecond
	for name, call := range map[string]func() error{
		"Success": func() error { return c.Success(context.Background()) },
		"Fail":    func() error { return c.Fail(context.Background(), "x") },
	} {
		start := time.Now()
		err := call()
		if err == nil {
			t.Fatalf("%s: got nil error from a server that never answers", name)
		}
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("%s: returned after %v, want about the timeout", name, d)
		}
		if strings.Contains(err.Error(), key) || strings.Contains(err.Error(), srv.URL) {
			t.Errorf("%s: error %q holds the ping URL", name, err)
		}
	}
}

func TestUnreachableServerErrorHasNoURL(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	err := New(srv.URL + key).Success(context.Background())
	if err == nil {
		t.Fatal("got nil error from a closed server")
	}
	if strings.Contains(err.Error(), key) || strings.Contains(err.Error(), srv.URL) {
		t.Errorf("error %q holds the ping URL", err)
	}
}
