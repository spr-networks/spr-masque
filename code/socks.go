package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// TraceURL is Cloudflare's connectivity/trace endpoint; fetched *through* the
// SOCKS5 proxy so it exercises the MASQUE tunnel end to end.
var TraceURL = "https://www.cloudflare.com/cdn-cgi/trace"

// dialSocks5 dials target (host:port) through a SOCKS5 proxy (no auth),
// implementing the minimal client side of RFC 1928 with the stdlib only.
func dialSocks5(ctx context.Context, proxyAddr, target string) (net.Conn, error) {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}

	fail := func(err error) (net.Conn, error) {
		conn.Close()
		return nil, err
	}

	// greeting: version 5, one method: no auth
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return fail(err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fail(err)
	}
	if resp[0] != 0x05 || resp[1] != 0x00 {
		return fail(fmt.Errorf("socks5: no acceptable auth method (got %d)", resp[1]))
	}

	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return fail(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return fail(fmt.Errorf("socks5: invalid target port %q", portStr))
	}

	// CONNECT request
	req := []byte{0x05, 0x01, 0x00}
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			req = append(req, 0x01)
			req = append(req, ip4...)
		} else {
			req = append(req, 0x04)
			req = append(req, ip.To16()...)
		}
	} else {
		if len(host) > 255 {
			return fail(fmt.Errorf("socks5: hostname too long"))
		}
		req = append(req, 0x03, byte(len(host)))
		req = append(req, host...)
	}
	req = append(req, byte(port>>8), byte(port))
	if _, err := conn.Write(req); err != nil {
		return fail(err)
	}

	// reply: VER REP RSV ATYP BND.ADDR BND.PORT
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return fail(err)
	}
	if head[0] != 0x05 || head[1] != 0x00 {
		return fail(fmt.Errorf("socks5: connect failed (rep=%d)", head[1]))
	}
	var bndLen int
	switch head[3] {
	case 0x01:
		bndLen = 4
	case 0x04:
		bndLen = 16
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(conn, l); err != nil {
			return fail(err)
		}
		bndLen = int(l[0])
	default:
		return fail(fmt.Errorf("socks5: bad reply address type %d", head[3]))
	}
	bnd := make([]byte, bndLen+2)
	if _, err := io.ReadFull(conn, bnd); err != nil {
		return fail(err)
	}

	conn.SetDeadline(time.Time{})
	return conn, nil
}

// fetchTrace GETs the Cloudflare trace URL through the SOCKS5 proxy and
// returns the raw body.
func fetchTrace(ctx context.Context, proxyAddr string) (string, error) {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialSocks5(ctx, proxyAddr, addr)
		},
		DisableKeepAlives: true,
	}
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	defer transport.CloseIdleConnections()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, TraceURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("trace returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// parseTrace parses Cloudflare's "key=value" trace lines.
func parseTrace(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		k, v, found := strings.Cut(line, "=")
		if found && k != "" {
			out[k] = v
		}
	}
	return out
}
