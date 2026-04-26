// Package netutil holds small networking helpers shared between SSO
// (issuer reachability tests) and alerts (webhook dispatch). Both
// previously open-coded the same private-IP refusal pattern; this
// package centralises it so a fix in one applies to both.
package netutil

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

// HostStatus is the trichotomy callers actually care about: a public
// reachable address, a private/loopback/link-local address, or a DNS
// failure that we can't classify (treat as untrustworthy by default
// but distinguishable for UX so the operator sees "DNS failed, retry"
// instead of "private address blocked").
type HostStatus int

const (
	HostPublic HostStatus = iota
	HostPrivate
	HostDNSFailed
)

// ClassifyHost reports whether `host` (literal IP or DNS name) is a
// public address, a private/loopback/link-local one, or DNS-failed.
// IP literals are classified directly; names go through net.LookupIP
// — any resolved address being private flips the status to private,
// otherwise public; lookup errors return HostDNSFailed.
func ClassifyHost(host string) HostStatus {
	check := func(ip net.IP) bool {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
	}
	if ip := net.ParseIP(host); ip != nil {
		if check(ip) {
			return HostPrivate
		}
		return HostPublic
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return HostDNSFailed
	}
	for _, ip := range ips {
		if check(ip) {
			return HostPrivate
		}
	}
	return HostPublic
}

// IsPrivateHost reports whether `host` (literal IP or DNS name) maps
// to a loopback / private / link-local / unspecified / multicast
// address. DNS lookup failure is treated as "private" — better to
// refuse to dial than to risk a DNS-rebinding race resolving to
// 127.0.0.1 a second later. Used by SafeHTTPClient.DialContext where
// we can't distinguish "DNS failed" from "private" at the network
// layer anyway. Validators that care about UX should call
// ClassifyHost instead.
func IsPrivateHost(host string) bool {
	return ClassifyHost(host) != HostPublic
}

// ErrPrivateAddressBlocked is returned by the dialer when the resolved
// IP is private and `allowPrivate` is false. Callers can errors.Is
// against it to distinguish from connect-refused / DNS errors.
var ErrPrivateAddressBlocked = errors.New("refusing to dial private address")

// SafeHTTPClient builds an http.Client whose transport refuses to
// connect to private/loopback/link-local addresses unless allowPrivate
// is true. The DialContext re-checks at TCP open time so DNS rebinding
// (a server returning a public IP at validation but private IP at
// dial) doesn't slip past validators that ran earlier.
//
// Timeouts are conservative defaults suited to webhook / OIDC discovery
// (5s dial, 5s TLS, 5s response header). For long downloads pass
// timeout=0 and override.
func SafeHTTPClient(allowPrivate bool, timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			if !allowPrivate && IsPrivateHost(host) {
				return nil, ErrPrivateAddressBlocked
			}
			return dialer.DialContext(ctx, network, addr)
		},
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	}
	if timeout == 0 {
		timeout = 8 * time.Second
	}
	return &http.Client{Transport: transport, Timeout: timeout}
}
