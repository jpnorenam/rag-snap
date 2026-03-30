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
)

// driveClientID and driveClientSecret are embedded at build time via -ldflags.
// They can be overridden at runtime with GOOGLE_DRIVE_CLIENT_ID /
// GOOGLE_DRIVE_CLIENT_SECRET, which is the recommended approach for development.
var (
	driveClientID     = ""
	driveClientSecret = ""
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

// resolveClientCredentials returns the OAuth2 client credentials, preferring
// env vars over the compile-time defaults.
func resolveClientCredentials() (clientID, clientSecret string, err error) {
	clientID = os.Getenv("GOOGLE_DRIVE_CLIENT_ID")
	if clientID == "" {
		clientID = driveClientID
	}
	clientSecret = os.Getenv("GOOGLE_DRIVE_CLIENT_SECRET")
	if clientSecret == "" {
		clientSecret = driveClientSecret
	}
	if clientID == "" || clientSecret == "" {
		return "", "", fmt.Errorf(
			"Google Drive OAuth2 credentials are not configured\n" +
				"  For development: set GOOGLE_DRIVE_CLIENT_ID and GOOGLE_DRIVE_CLIENT_SECRET\n" +
				"  For production:  embed them at build time via make DRIVE_CLIENT_ID=... DRIVE_CLIENT_SECRET=...",
		)
	}
	return clientID, clientSecret, nil
}

// driveTokenCachePath returns the path of the cached token file.
// Uses $SNAP_USER_DATA when running as a snap, otherwise ~/.config/rag-snap/.
func driveTokenCachePath() (string, error) {
	var dir string
	if snapData := os.Getenv("SNAP_USER_DATA"); snapData != "" {
		dir = snapData
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locating home directory: %w", err)
		}
		dir = filepath.Join(home, ".config", "rag-snap")
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

// runLoopbackFlow executes the OAuth2 Authorization Code flow with PKCE using a
// loopback redirect URI. It starts a temporary HTTP server on a random localhost
// port, opens the user's browser, waits for the callback, then exchanges the
// authorization code for tokens.
//
// This flow is required for the drive.readonly scope, which Google does not
// permit via the device authorization grant. Use a "Desktop app" OAuth2 client.
func runLoopbackFlow(ctx context.Context, clientID, clientSecret string) (*DriveToken, error) {
	verifier, err := pkceVerifier()
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
		"code_challenge":        {pkceChallenge(verifier)},
		"code_challenge_method": {"S256"},
	}
	fullAuthURL := driveAuthURL + "?" + authParams.Encode()

	fmt.Printf("\nTo authenticate with Google Drive, open the following URL in your browser:\n\n")
	fmt.Printf("  %s\n\n", fullAuthURL)
	fmt.Printf("Attempting to open your browser automatically...\n")
	openBrowser(fullAuthURL)
	fmt.Printf("Waiting for authorization")

	// codeCh receives the authorization code from the callback handler.
	type callbackResult struct {
		code string
		err  error
	}
	resultCh := make(chan callbackResult, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if errParam := r.URL.Query().Get("error"); errParam != "" {
			desc := r.URL.Query().Get("error_description")
			http.Error(w, "Authentication failed: "+errParam, http.StatusBadRequest)
			resultCh <- callbackResult{err: fmt.Errorf("authorization denied: %s (%s)", errParam, desc)}
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Missing authorization code", http.StatusBadRequest)
			resultCh <- callbackResult{err: fmt.Errorf("callback received no authorization code")}
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body><h2>Authentication successful!</h2><p>You may close this window and return to the terminal.</p></body></html>")
		resultCh <- callbackResult{code: code}
	})

	srv := &http.Server{Handler: mux}
	go srv.Serve(ln) //nolint:errcheck

	// Poll for the dot animation while waiting.
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	timeout := time.NewTimer(5 * time.Minute)
	defer timeout.Stop()

	var authCode string
	for {
		select {
		case res := <-resultCh:
			fmt.Println(" authorized.")
			_ = srv.Shutdown(context.Background())
			if res.err != nil {
				return nil, res.err
			}
			authCode = res.code
			goto exchange
		case <-ticker.C:
			fmt.Print(".")
		case <-timeout.C:
			_ = srv.Shutdown(context.Background())
			fmt.Println()
			return nil, fmt.Errorf("authentication timed out — no response received within 5 minutes")
		case <-ctx.Done():
			_ = srv.Shutdown(context.Background())
			fmt.Println()
			return nil, ctx.Err()
		}
	}

exchange:
	return exchangeCode(ctx, authCode, redirectURI, verifier, clientID, clientSecret)
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
func LoadOrAuthenticateDrive(ctx context.Context) (string, error) {
	clientID, clientSecret, err := resolveClientCredentials()
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
