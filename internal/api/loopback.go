package api

import (
	"context"
	"fmt"
	"net"
)

// loopbackConn marks a connection accepted on the UI listener so the auth
// middleware authenticates it via the localhost token rather than SO_PEERCRED
// (which is unavailable for TCP). It mirrors how credConn tags unix peers.
type loopbackConn struct {
	net.Conn
}

// loopbackListener wraps the TCP listener so every accepted connection is a
// loopbackConn. http.Server.ConnContext (connContext) then stamps the loopback
// marker onto each request's context.
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

// listenLoopback opens a TCP listener on cfg.Address after verifying it resolves
// to a loopback address. It refuses any non-loopback bind in this change
// (rest-api-ui-serving: "Non-loopback bind refused"), so the UI can never be
// exposed remotely here.
func listenLoopback(cfg UIConfig) (net.Listener, error) {
	host, _, err := net.SplitHostPort(cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("invalid api.ui.address %q: %w", cfg.Address, err)
	}
	if err := requireLoopbackHost(host); err != nil {
		return nil, err
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", cfg.Address, err)
	}

	// Defence in depth: confirm the bound address is loopback in case the host
	// resolved to mixed addresses.
	if tcpAddr, ok := ln.Addr().(*net.TCPAddr); ok && !tcpAddr.IP.IsLoopback() {
		_ = ln.Close()
		return nil, fmt.Errorf("refusing non-loopback UI bind: resolved to %s", tcpAddr.IP)
	}

	return loopbackListener{Listener: ln}, nil
}

// requireLoopbackHost returns an error unless host is a loopback address (or an
// empty/loopback hostname). An empty host (e.g. ":8080") binds all interfaces
// and is rejected.
func requireLoopbackHost(host string) error {
	if host == "" {
		return fmt.Errorf("refusing UI bind to all interfaces: api.ui.address must be a loopback address (e.g. 127.0.0.1:0)")
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("refusing UI bind: api.ui.address host %q is not a loopback IP", host)
	}
	if !ip.IsLoopback() {
		return fmt.Errorf("refusing non-loopback UI bind: %q is not a loopback address", host)
	}
	return nil
}
