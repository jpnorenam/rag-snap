package knowledge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/jpnorenam/rag-snap/pkg/storage"
)

const (
	driveScope    = "https://www.googleapis.com/auth/drive.readonly"
	driveAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	driveTokenURL = "https://oauth2.googleapis.com/token"
)

// DriveToken is the persisted OAuth2 token for Drive API access.
type DriveToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// valid returns true when the token is present and not within 30 seconds of expiry.
func (t *DriveToken) valid() bool {
	return t != nil && t.AccessToken != "" && time.Now().Before(t.ExpiresAt.Add(-30*time.Second))
}

// resolveClientCredentials returns the OAuth2 client credentials.
// Resolution order: env vars → snap config (gdrive.client.id / gdrive.client.secret).
func resolveClientCredentials(cfg storage.Config) (clientID, clientSecret string, err error) {
	clientID = os.Getenv("GOOGLE_DRIVE_CLIENT_ID")
	clientSecret = os.Getenv("GOOGLE_DRIVE_CLIENT_SECRET")

	if clientID == "" {
		clientID, _ = configString(cfg, "gdrive.client.id")
	}
	if clientSecret == "" {
		clientSecret, _ = configString(cfg, "gdrive.client.secret")
	}

	if clientID == "" || clientSecret == "" {
		return "", "", fmt.Errorf(
			"Google Drive OAuth2 credentials are not configured\n" +
				"  Run: sudo rag set gdrive.client.id=<client-id>\n" +
				"       sudo rag set gdrive.client.secret=<client-secret>\n" +
				"  Or set GOOGLE_DRIVE_CLIENT_ID / GOOGLE_DRIVE_CLIENT_SECRET environment variables",
		)
	}
	return clientID, clientSecret, nil
}

// configString reads a single string value from snap config. Returns ("", false) when absent.
func configString(cfg storage.Config, key string) (string, bool) {
	vals, err := cfg.Get(key)
	if err != nil || len(vals) == 0 {
		return "", false
	}
	v, ok := vals[key]
	if !ok {
		return "", false
	}
	s := fmt.Sprint(v)
	return s, s != ""
}

// driveTokenCachePath returns the path of the cached token file.
// Uses $SNAP_USER_DATA when running as a snap, otherwise ~/.config/rag-cli/.
func driveTokenCachePath() (string, error) {
	var dir string
	if snapData := os.Getenv("SNAP_USER_DATA"); snapData != "" {
		dir = snapData
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locating home directory: %w", err)
		}
		dir = filepath.Join(home, ".config", "rag-cli")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("creating token cache directory: %w", err)
	}
	return filepath.Join(dir, "gdrive-token.json"), nil
}

func loadCachedDriveToken() (*DriveToken, error) {
	path, err := driveTokenCachePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil // no cached token; not an error
	}
	if err != nil {
		return nil, fmt.Errorf("reading token cache: %w", err)
	}
	var tok DriveToken
	if err := json.Unmarshal(data, &tok); err != nil {
		// Corrupted cache — treat as missing so we re-authenticate cleanly.
		return nil, nil
	}
	return &tok, nil
}

func saveDriveToken(tok *DriveToken) error {
	path, err := driveTokenCachePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling token: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

// tokenResponse covers both success and error payloads from the token endpoint.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// pkceVerifier generates a cryptographically random PKCE code verifier.
func pkceVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating PKCE verifier: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// pkceChallenge derives the S256 code challenge from a verifier.
func pkceChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// openBrowser attempts to open url in the user's default browser.
func openBrowser(rawURL string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "linux":
		cmd, args = "xdg-open", []string{rawURL}
	case "darwin":
		cmd, args = "open", []string{rawURL}
	default:
		return
	}
	_ = exec.Command(cmd, args...).Start()
}

// driveFlowTimeout bounds how long a loopback OAuth flow waits for the user to
// complete Google's consent screen before giving up.
const driveFlowTimeout = 5 * time.Minute

// callbackResult carries the authorization code (or an error) from the loopback
// callback handler back to the awaiting caller.
type callbackResult struct {
	code string
	err  error
}

// DriveOAuthFlow is an in-progress OAuth2 Authorization-Code-with-PKCE loopback
// flow. It binds an ephemeral loopback callback listener and builds Google's
// consent URL; the caller opens that URL in a browser and then blocks on Await
// for the redirect. The flow is single-use: Await releases the listener.
//
// This loopback flow is required for the drive.readonly scope, which Google does
// not permit via the device authorization grant. Use a "Desktop app" client.
type DriveOAuthFlow struct {
	clientID     string
	clientSecret string
	verifier     string
	state        string
	redirectURI  string
	consentURL   string
	srv          *http.Server
	resultCh     chan callbackResult
}

// StartDriveOAuthFlow resolves the configured client credentials and starts a
// loopback OAuth flow: it binds an ephemeral 127.0.0.1 callback listener, builds
// the consent URL, and begins serving the callback in the background. The daemon
// uses this to mediate consent on behalf of the browser; the CLI uses the
// lower-level startDriveOAuthFlow directly.
func StartDriveOAuthFlow(cfg storage.Config) (*DriveOAuthFlow, error) {
	clientID, clientSecret, err := resolveClientCredentials(cfg)
	if err != nil {
		return nil, err
	}
	return startDriveOAuthFlow(clientID, clientSecret)
}

// startDriveOAuthFlow binds the callback listener, generates the PKCE verifier
// and a state nonce, builds the consent URL, and starts serving the callback.
func startDriveOAuthFlow(clientID, clientSecret string) (*DriveOAuthFlow, error) {
	verifier, err := pkceVerifier()
	if err != nil {
		return nil, err
	}
	// The state nonce guards the callback against cross-flow/CSRF confusion.
	state, err := pkceVerifier()
	if err != nil {
		return nil, err
	}

	// Bind to a random free port on loopback.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("starting local callback server: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d", port)

	authParams := url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {driveScope},
		"access_type":           {"offline"},
		"prompt":                {"consent"}, // ensures a refresh token is returned
		"state":                 {state},
		"code_challenge":        {pkceChallenge(verifier)},
		"code_challenge_method": {"S256"},
	}

	f := &DriveOAuthFlow{
		clientID:     clientID,
		clientSecret: clientSecret,
		verifier:     verifier,
		state:        state,
		redirectURI:  redirectURI,
		consentURL:   driveAuthURL + "?" + authParams.Encode(),
		resultCh:     make(chan callbackResult, 1),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", f.handleCallback)
	f.srv = &http.Server{Handler: mux}
	go f.srv.Serve(ln) //nolint:errcheck

	return f, nil
}

// handleCallback validates the state nonce and extracts the authorization code
// (or an error) from Google's redirect, forwarding it to Await via resultCh.
func (f *DriveOAuthFlow) handleCallback(w http.ResponseWriter, r *http.Request) {
	if st := r.URL.Query().Get("state"); st != f.state {
		http.Error(w, "Invalid state parameter", http.StatusBadRequest)
		f.resultCh <- callbackResult{err: fmt.Errorf("callback state mismatch")}
		return
	}
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		desc := r.URL.Query().Get("error_description")
		http.Error(w, "Authentication failed: "+errParam, http.StatusBadRequest)
		f.resultCh <- callbackResult{err: fmt.Errorf("authorization denied: %s (%s)", errParam, desc)}
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		f.resultCh <- callbackResult{err: fmt.Errorf("callback received no authorization code")}
		return
	}
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, "<html><body><h2>Authentication successful!</h2><p>You may close this window and return to the application.</p></body></html>")
	f.resultCh <- callbackResult{code: code}
}

// ConsentURL is the Google consent URL to open in a browser. It carries no
// client secret (PKCE keeps that server-side) and is safe to hand to the UI.
func (f *DriveOAuthFlow) ConsentURL() string { return f.consentURL }

// Close shuts down the callback listener. It is safe to call more than once.
func (f *DriveOAuthFlow) Close() {
	if f.srv != nil {
		_ = f.srv.Shutdown(context.Background())
	}
}

// Await blocks until the callback is received, the context is cancelled, or the
// timeout elapses, then exchanges the authorization code for a token. It always
// releases the callback listener before returning.
func (f *DriveOAuthFlow) Await(ctx context.Context, timeout time.Duration) (*DriveToken, error) {
	defer f.Close()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var code string
	select {
	case res := <-f.resultCh:
		if res.err != nil {
			return nil, res.err
		}
		code = res.code
	case <-timer.C:
		return nil, fmt.Errorf("authentication timed out — no response received within %s", timeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	return exchangeCode(ctx, code, f.redirectURI, f.verifier, f.clientID, f.clientSecret)
}

// runLoopbackFlow drives the loopback OAuth flow interactively for the CLI:
// it prints the consent URL, opens the browser, animates a wait, then blocks
// for the callback.
func runLoopbackFlow(ctx context.Context, clientID, clientSecret string) (*DriveToken, error) {
	flow, err := startDriveOAuthFlow(clientID, clientSecret)
	if err != nil {
		return nil, err
	}

	fmt.Printf("\nTo authenticate with Google Drive, open the following URL in your browser:\n\n")
	fmt.Printf("  %s\n\n", flow.ConsentURL())
	fmt.Printf("Attempting to open your browser automatically...\n")
	openBrowser(flow.ConsentURL())
	fmt.Printf("Waiting for authorization")

	// Animate dots on a ticker until Await returns.
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				fmt.Print(".")
			}
		}
	}()

	tok, err := flow.Await(ctx, driveFlowTimeout)
	close(done)
	if err != nil {
		fmt.Println()
		return nil, err
	}
	fmt.Println(" authorized.")
	return tok, nil
}

// exchangeCode trades an authorization code for an access + refresh token pair.
func exchangeCode(ctx context.Context, code, redirectURI, verifier, clientID, clientSecret string) (*DriveToken, error) {
	body := url.Values{
		"code":          {code},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
		"code_verifier": {verifier},
	}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, driveTokenURL, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building token exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("exchanging authorization code: %w", err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var tr tokenResponse
	if err := json.Unmarshal(respBody, &tr); err != nil {
		return nil, fmt.Errorf("parsing token response: %w", err)
	}
	if tr.Error != "" {
		return nil, fmt.Errorf("token exchange failed: %s (%s)", tr.Error, tr.ErrorDesc)
	}

	return &DriveToken{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
	}, nil
}

// refreshDriveToken exchanges a refresh token for a new access token.
func refreshDriveToken(ctx context.Context, tok *DriveToken, clientID, clientSecret string) (*DriveToken, error) {
	body := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"refresh_token": {tok.RefreshToken},
		"grant_type":    {"refresh_token"},
	}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, driveTokenURL, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("refreshing token: %w", err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var tr tokenResponse
	if err := json.Unmarshal(respBody, &tr); err != nil {
		return nil, fmt.Errorf("parsing refresh response: %w", err)
	}
	if tr.Error != "" {
		return nil, fmt.Errorf("token refresh failed: %s (%s)", tr.Error, tr.ErrorDesc)
	}

	// Google does not always return a new refresh token — preserve the existing one.
	refreshToken := tr.RefreshToken
	if refreshToken == "" {
		refreshToken = tok.RefreshToken
	}
	return &DriveToken{
		AccessToken:  tr.AccessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
	}, nil
}

// LoadOrAuthenticateDrive returns a valid Drive access token. It uses the
// following strategy in order:
//  1. Return the cached token if it is still valid.
//  2. Silently refresh using the stored refresh token.
//  3. Run the full loopback Authorization Code + PKCE flow (opens a browser).
func LoadOrAuthenticateDrive(ctx context.Context, cfg storage.Config) (string, error) {
	clientID, clientSecret, err := resolveClientCredentials(cfg)
	if err != nil {
		return "", err
	}

	tok, err := loadCachedDriveToken()
	if err != nil {
		return "", err
	}

	if tok.valid() {
		return tok.AccessToken, nil
	}

	// Silent refresh when a refresh token is available.
	if tok != nil && tok.RefreshToken != "" {
		refreshed, err := refreshDriveToken(ctx, tok, clientID, clientSecret)
		if err == nil {
			_ = saveDriveToken(refreshed)
			return refreshed.AccessToken, nil
		}
		// Refresh token revoked or expired — fall through to full flow.
	}

	// Full browser-based flow.
	newTok, err := runLoopbackFlow(ctx, clientID, clientSecret)
	if err != nil {
		return "", err
	}
	_ = saveDriveToken(newTok)
	return newTok.AccessToken, nil
}

// SaveDriveToken persists a Drive token to the on-disk cache. The daemon calls
// this after completing an OAuth flow.
func SaveDriveToken(tok *DriveToken) error { return saveDriveToken(tok) }

// DriveConfigured reports whether Drive OAuth client credentials are available
// (env vars or gdrive.client.id / gdrive.client.secret config).
func DriveConfigured(cfg storage.Config) bool {
	_, _, err := resolveClientCredentials(cfg)
	return err == nil
}

// DriveAccessToken returns a valid Drive access token using only the cache and a
// silent refresh — it never starts a browser flow. It returns ("", nil) when no
// usable token is stored, so callers can distinguish "not connected" (empty
// string, nil error) from a hard failure (non-nil error). Credentials must be
// configured; an unconfigured daemon gets the resolve error.
func DriveAccessToken(ctx context.Context, cfg storage.Config) (string, error) {
	clientID, clientSecret, err := resolveClientCredentials(cfg)
	if err != nil {
		return "", err
	}

	tok, err := loadCachedDriveToken()
	if err != nil {
		return "", err
	}
	if tok.valid() {
		return tok.AccessToken, nil
	}
	if tok != nil && tok.RefreshToken != "" {
		refreshed, err := refreshDriveToken(ctx, tok, clientID, clientSecret)
		if err == nil {
			_ = saveDriveToken(refreshed)
			return refreshed.AccessToken, nil
		}
		// Refresh token revoked or expired — treat as not connected.
	}
	return "", nil
}

// DeleteDriveToken removes the cached Drive token, disconnecting the account. A
// missing token is not an error.
func DeleteDriveToken() error {
	path, err := driveTokenCachePath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deleting token cache: %w", err)
	}
	return nil
}
