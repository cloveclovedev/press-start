// Package google holds the code that talks to Google: the OAuth client and
// the loopback flow that gets the refresh token.
//
// The flow follows the Google guide for installed apps
// (https://developers.google.com/identity/protocols/oauth2/native-app):
// a loopback redirect on 127.0.0.1 and PKCE with S256. Google blocks the
// out-of-band flow, and the device flow does not allow Calendar scopes.
package google

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"

	"golang.org/x/oauth2"
)

// CalendarScope lets the service read the events of the calendars, and
// nothing else.
const CalendarScope = "https://www.googleapis.com/auth/calendar.events.readonly"

// clientFile is the JSON file of an OAuth client that Google Cloud gives.
// A client of the type "Desktop app" has its values under "installed".
type clientFile struct {
	Installed *struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		AuthURI      string `json:"auth_uri"`
		TokenURI     string `json:"token_uri"`
	} `json:"installed"`
}

// LoadConfig reads the OAuth client JSON file at path. The client must be of
// the type "Desktop app". The config asks for CalendarScope only.
func LoadConfig(path string) (*oauth2.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read OAuth client: %w", err)
	}
	return ConfigFromJSON(data)
}

// ConfigFromJSON is LoadConfig for the content of the file.
func ConfigFromJSON(data []byte) (*oauth2.Config, error) {
	var f clientFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse OAuth client: %w", err)
	}
	c := f.Installed
	if c == nil {
		return nil, errors.New(`parse OAuth client: no "installed" client; create a client of the type "Desktop app"`)
	}
	if c.ClientID == "" || c.AuthURI == "" || c.TokenURI == "" {
		return nil, errors.New("parse OAuth client: client_id, auth_uri, or token_uri is empty")
	}
	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  c.AuthURI,
			TokenURL: c.TokenURI,
			// Google takes the client in the form, as the Google library does.
			AuthStyle: oauth2.AuthStyleInParams,
		},
		Scopes: []string{CalendarScope},
	}, nil
}

// callback is what the redirect of the browser brings back.
type callback struct {
	code string
	err  error
}

// RefreshToken runs the loopback flow on ln, which must listen on 127.0.0.1.
// It gives the consent URL to showURL, waits for the redirect of the browser,
// and exchanges the code for a token. It returns the refresh token, and
// writes nothing to a file. It returns an error when ctx ends first.
func RefreshToken(ctx context.Context, cfg *oauth2.Config, ln net.Listener, showURL func(string)) (string, error) {
	// The state ties the redirect to this run.
	state := rand.Text()
	verifier := oauth2.GenerateVerifier()
	c := *cfg
	c.RedirectURL = fmt.Sprintf("http://127.0.0.1:%d/", ln.Addr().(*net.TCPAddr).Port)

	// The browser may send more than one request, such as for an icon. Only
	// the first request on "/" counts.
	result := make(chan callback, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		cb := readCallback(r, state)
		select {
		case result <- cb:
		default:
			http.Error(w, "press-start: the authorization already ended", http.StatusConflict)
			return
		}
		if cb.err != nil {
			http.Error(w, "press-start: authorization failed. See the terminal.", http.StatusBadRequest)
			return
		}
		fmt.Fprintln(w, "press-start: authorization received. You can close this tab.")
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()

	// access_type=offline and prompt=consent make Google return a refresh
	// token, also when the user gave consent before.
	showURL(c.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce, oauth2.S256ChallengeOption(verifier)))

	var cb callback
	select {
	case cb = <-result:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if cb.err != nil {
		return "", cb.err
	}
	tok, err := c.Exchange(ctx, cb.code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", fmt.Errorf("exchange the code: %w", err)
	}
	if tok.RefreshToken == "" {
		return "", errors.New("exchange the code: Google returned no refresh token")
	}
	return tok.RefreshToken, nil
}

// readCallback checks the state of the redirect and takes the code from it.
func readCallback(r *http.Request, state string) callback {
	q := r.URL.Query()
	if q.Get("state") != state {
		return callback{err: errors.New("authorization: the state of the redirect does not match")}
	}
	if e := q.Get("error"); e != "" {
		return callback{err: fmt.Errorf("authorization: Google returned the error %q", e)}
	}
	code := q.Get("code")
	if code == "" {
		return callback{err: errors.New("authorization: the redirect has no code")}
	}
	return callback{code: code}
}
