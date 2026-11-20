package common

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"syscall"
	"time"
)

// handshakeTimeout bounds the TCP dial a Handshake makes.
const handshakeTimeout = 5 * time.Second

// Handshake checks that the server behind baseURL accepts TCP connections,
// showing a spinner labeled label while it dials. When baseURL carries no port,
// "443" is used for https and defaultHTTPPort otherwise. A refused connection is
// reported with the server-startup and server-logs suggestions.
func Handshake(ctx context.Context, baseURL, label, defaultHTTPPort string) error {
	stopProgress := StartProgressSpinner(label)
	defer stopProgress()

	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("invalid base URL: %w", err)
	}
	port := parsedURL.Port()
	if port == "" {
		if parsedURL.Scheme == "https" {
			port = "443"
		} else {
			port = defaultHTTPPort
		}
	}

	dialer := &net.Dialer{Timeout: handshakeTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(parsedURL.Hostname(), port))
	if errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("connection refused\n\n%s\n%s", SuggestServerStartup(), SuggestServerLogs())
	} else if err != nil {
		return err
	}
	return conn.Close()
}
