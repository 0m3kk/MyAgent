package tools

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestProxyMocksAndAllowlist(t *testing.T) {
	proxy, err := StartProxy([]string{"example.com"}, nil, map[string]MockResponse{
		"GET http://example.com/data": {Status: 200, JSON: map[string]any{"ok": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}

	resp, err := client.Get("http://example.com/data")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"ok":true`) {
		t.Fatalf("mock response: %d %s", resp.StatusCode, body)
	}

	resp, err = client.Get("http://evil.example.org/data")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestProxyBlocksPrivateTargets(t *testing.T) {
	proxy, err := StartProxy([]string{"127.0.0.1"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	resp, err := client.Get("http://127.0.0.1:9999/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", resp.StatusCode)
	}
}
