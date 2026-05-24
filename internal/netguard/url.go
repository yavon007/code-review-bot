package netguard

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

var ErrUnsafeURL = errors.New("unsafe public url")

func ValidatePublicHTTPSBaseURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	return ValidatePublicHTTPSURL(parsed)
}

func ValidatePublicHTTPSURL(parsed *url.URL) error {
	if parsed == nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return ErrUnsafeURL
	}
	host := parsed.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicIP(ip) {
			return ErrUnsafeURL
		}
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("resolve public url host: %w", err)
	}
	if len(ips) == 0 {
		return ErrUnsafeURL
	}
	for _, ip := range ips {
		if !isPublicIP(ip) {
			return ErrUnsafeURL
		}
	}
	return nil
}

func PublicHTTPSRedirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	return ValidatePublicHTTPSURL(req.URL)
}

func isPublicIP(ip net.IP) bool {
	return !(ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast())
}
