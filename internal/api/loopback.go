package api

import (
	"context"
	"fmt"
	"net"
)

// loopbackConn tags an accepted TCP connection as belonging to the loopback
// listener. connContext reads this type to stamp transportLoopback onto the
// request context, which routes the request to token authentication instead of
// the peercred check used for the unix socket.
type loopbackConn struct {
	net.Conn
}

// loopbackListener wraps a TCP net.Listener so every accepted connection is a
// loopbackConn. This mirrors peerCredListener for the unix socket: the wrapper
// is the seam that carries the transport identity from accept time into the HTTP
// server's ConnContext.
type loopbackListener struct {
	net.Listener
}

func (l loopbackListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &loopbackConn{Conn: conn}, nil
}

// listenLoopback validates the configured host and binds a TCP listener on it,
// refusing any non-loopback bind. It performs the check twice: once on the
// configured host before listening, and once on the resolved *net.TCPAddr after
// binding (defence in depth against a hostname that resolves to a mix of
// addresses). Every accepted connection is wrapped as a loopbackConn.
func listenLoopback(cfg LoopbackConfig) (net.Listener, error) {
	host, _, err := net.SplitHostPort(cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("parsing loopback address %q: %w", cfg.Address, err)
	}
	if err := requireLoopbackHost(host); err != nil {
		return nil, err
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", cfg.Address, err)
	}

	// Post-bind defence in depth: refuse to serve if the OS bound us to a
	// non-loopback address after all (e.g. a host that resolved unexpectedly).
	if tcpAddr, ok := ln.Addr().(*net.TCPAddr); ok {
		if !tcpAddr.IP.IsLoopback() {
			_ = ln.Close()
			return nil, fmt.Errorf("refusing to serve loopback API on non-loopback address %s", tcpAddr)
		}
	}

	return loopbackListener{Listener: ln}, nil
}

// requireLoopbackHost reports whether host is a loopback bind target. An empty
// host means "all interfaces" and is refused; the literal "localhost" is
// accepted; an IP is accepted only if it is a loopback address. This guards the
// configured api.loopback.address before any port is bound.
func requireLoopbackHost(host string) error {
	if host == "" {
		return fmt.Errorf("loopback address must specify a loopback host (an empty host binds all interfaces); use 127.0.0.1 or ::1")
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("loopback host %q is not a loopback IP or \"localhost\"", host)
	}
	if !ip.IsLoopback() {
		return fmt.Errorf("refusing non-loopback bind host %q; only loopback addresses are allowed", host)
	}
	return nil
}
