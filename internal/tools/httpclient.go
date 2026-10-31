package tools

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"
)

// SafeHTTPClient refuses to connect to loopback, private, link-local and
// unspecified addresses. This protects the agent against SSRF from fetched
// content and from URLs it invents.
func safeDialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 15 * time.Second,
		Control: func(network, address string, c syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil {
				return fmt.Errorf("refusing to connect to unresolved address %s", address)
			}
			if blockedIP(ip) {
				return fmt.Errorf("refusing to connect to private address %s", ip.String())
			}
			return nil
		},
	}
}

func safeTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           safeDialer().DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
}

func SafeHTTPClient() *http.Client {
	return &http.Client{
		Transport: safeTransport(),
		Timeout:   60 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("refusing redirect to %s", req.URL.Scheme)
			}
			return nil
		},
	}
}

func blockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return true
		}
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
	}
	return false
}

func validateFetchURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("only http and https URLs are allowed")
	}
	if u.Host == "" {
		return nil, fmt.Errorf("URL has no host")
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil && blockedIP(ip) {
		return nil, fmt.Errorf("private addresses are not allowed")
	}
	return u, nil
}
