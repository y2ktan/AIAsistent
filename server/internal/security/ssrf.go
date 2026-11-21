package security

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

var (
	ErrBlockedSSRF = errors.New("outbound URL target is blocked by SSRF policy")
	ErrInvalidURL  = errors.New("invalid outbound URL")
)

var blockedCIDRs []*net.IPNet

func init() {
	cidrs := []string{
		"0.0.0.0/8",      // Current network
		"10.0.0.0/8",     // RFC1918
		"100.64.0.0/10",  // CGNAT
		"127.0.0.0/8",    // Loopback
		"169.254.0.0/16", // Link-local / Cloud Metadata
		"172.16.0.0/12",  // RFC1918
		"192.0.0.0/24",   // IETF Protocol Assignments
		"192.168.0.0/16", // RFC1918
		"224.0.0.0/4",    // Multicast
		"240.0.0.0/4",    // Reserved
		"::/128",         // Unspecified IPv6
		"::1/128",        // Loopback IPv6
		"fc00::/7",       // Unique local IPv6
		"fe80::/10",      // Link-local IPv6
		"ff00::/8",       // Multicast IPv6
	}
	for _, c := range cidrs {
		_, ipNet, err := net.ParseCIDR(c)
		if err == nil {
			blockedCIDRs = append(blockedCIDRs, ipNet)
		}
	}
}

func IsBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	for _, cidr := range blockedCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

type SSRFPolicy struct {
	AllowPrivate bool
}

func (p SSRFPolicy) ValidateHTTPURL(rawURL string) (*url.URL, error) {
	return p.validateURL(rawURL, []string{"http", "https"})
}

func (p SSRFPolicy) ValidateWebSocketURL(rawURL string) (*url.URL, error) {
	return p.validateURL(rawURL, []string{"ws", "wss", "http", "https"})
}

func (p SSRFPolicy) validateURL(rawURL string, allowedSchemes []string) (*url.URL, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, ErrInvalidURL
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Hostname() == "" {
		return nil, ErrInvalidURL
	}
	schemeOK := false
	schemeLower := strings.ToLower(parsed.Scheme)
	for _, s := range allowedSchemes {
		if schemeLower == s {
			schemeOK = true
			break
		}
	}
	if !schemeOK {
		return nil, fmt.Errorf("%w: unsupported scheme %q", ErrInvalidURL, parsed.Scheme)
	}
	if p.AllowPrivate {
		return parsed, nil
	}

	host := parsed.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if IsBlockedIP(ip) {
			return nil, fmt.Errorf("%w: %s", ErrBlockedSSRF, host)
		}
		return parsed, nil
	}

	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".local") || strings.HasSuffix(strings.ToLower(host), ".internal") {
		return nil, fmt.Errorf("%w: %s", ErrBlockedSSRF, host)
	}

	ips, err := net.DefaultResolver.LookupIP(context.Background(), "ip", host)
	if err == nil {
		for _, ip := range ips {
			if IsBlockedIP(ip) {
				return nil, fmt.Errorf("%w: resolved to blocked IP %s", ErrBlockedSSRF, ip.String())
			}
		}
	}
	return parsed, nil
}

func NewSafeDialer(allowPrivate bool, timeout time.Duration) *net.Dialer {
	d := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
	}
	if !allowPrivate {
		d.Control = func(network, address string, c syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return fmt.Errorf("%w: invalid address", ErrBlockedSSRF)
			}
			ip := net.ParseIP(host)
			if ip == nil || IsBlockedIP(ip) {
				return fmt.Errorf("%w: %s", ErrBlockedSSRF, host)
			}
			return nil
		}
	}
	return d
}

func NewSafeHTTPClient(allowPrivate bool, timeout time.Duration) *http.Client {
	dialer := NewSafeDialer(allowPrivate, 10*time.Second)
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	policy := SSRFPolicy{AllowPrivate: allowPrivate}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("stopped after 5 redirects")
			}
			if _, err := policy.ValidateHTTPURL(req.URL.String()); err != nil {
				return err
			}
			return nil
		},
	}
}
