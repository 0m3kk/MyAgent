package tools

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// HeaderSecret is injected by the local proxy for matching domains. It never
// enters the script environment.
type HeaderSecret struct {
	Name    string
	Value   string
	Header  string
	Domains []string
}

// LocalProxy is a loopback HTTP/CONNECT proxy that enforces a domain allowlist,
// blocks private IPs, injects header secrets for plain HTTP and can serve test
// mocks. HTTPS CONNECT traffic is domain-checked but cannot have headers
// injected without a MITM certificate.
type LocalProxy struct {
	URL      string
	server   *http.Server
	listener net.Listener
	once     sync.Once
}

func StartProxy(
	domains []string,
	headers []HeaderSecret,
	mocks map[string]MockResponse,
) (*LocalProxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &LocalProxy{listener: ln, URL: "http://" + ln.Addr().String()}
	transport := safeTransport()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			host := r.Host
			if err := checkDomain(domains, "https://"+host); err != nil {
				http.Error(w, err.Error(), http.StatusForbidden)
				return
			}
			target, err := safeDialer().DialContext(r.Context(), "tcp", host)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				target.Close()
				http.Error(w, "hijacking not supported", http.StatusInternalServerError)
				return
			}
			client, _, err := hijacker.Hijack()
			if err != nil {
				target.Close()
				return
			}
			_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
			go tunnel(client, target)
			return
		}
		if r.URL == nil || r.URL.Host == "" {
			http.Error(w, "proxy requires an absolute URL", http.StatusBadRequest)
			return
		}
		rawURL := r.URL.String()
		if err := checkDomain(domains, rawURL); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		if resp, ok := mockFor(mocks, r.Method, rawURL); ok {
			writeMockHTTP(w, resp)
			return
		}
		host := strings.ToLower(r.URL.Hostname())
		for _, hs := range headers {
			if domainMatches(hs.Domains, host) {
				line := strings.ReplaceAll(hs.Header, "${value}", hs.Value)
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					r.Header.Set(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
				}
			}
		}
		// Strip hop-by-hop proxy headers.
		r.Header.Del("Proxy-Connection")
		r.Header.Del("Proxy-Authorization")
		out := r.Clone(r.Context())
		out.RequestURI = ""
		out.Header = r.Header.Clone()
		resp, err := transport.RoundTrip(out)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, vs := range resp.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, io.LimitReader(resp.Body, 8<<20))
	})
	p.server = &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = p.server.Serve(ln) }()
	return p, nil
}

func (p *LocalProxy) Close() {
	p.once.Do(func() {
		_ = p.server.Close()
		_ = p.listener.Close()
	})
}

func tunnel(a, b net.Conn) {
	defer a.Close()
	defer b.Close()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { _, _ = io.Copy(a, b); wg.Done() }()
	go func() { _, _ = io.Copy(b, a); wg.Done() }()
	wg.Wait()
}

func writeMockHTTP(w http.ResponseWriter, resp MockResponse) {
	body := resp.Body
	if resp.JSON != nil {
		raw, _ := json.Marshal(resp.JSON)
		body = string(raw)
	}
	status := resp.Status
	if status == 0 {
		status = 200
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-MyAgent-Mock", "1")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func domainMatches(domains []string, host string) bool {
	host = strings.ToLower(host)
	for _, d := range domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// ProxyEnv returns environment entries that route common HTTP clients through
// the local proxy.
func (p *LocalProxy) ProxyEnv() []string {
	return []string{
		"HTTP_PROXY=" + p.URL,
		"HTTPS_PROXY=" + p.URL,
		"ALL_PROXY=" + p.URL,
		"http_proxy=" + p.URL,
		"https_proxy=" + p.URL,
		"NO_PROXY=127.0.0.1,localhost",
		"no_proxy=127.0.0.1,localhost",
	}
}
