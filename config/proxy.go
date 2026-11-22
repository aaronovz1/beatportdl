package config

import (
	"errors"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// ProxyURL validates the configured proxy without including credentials in errors.
func (c *AppConfig) ProxyURL() (*url.URL, error) {
	if c.Proxy == "" {
		return nil, nil
	}
	proxy, err := url.Parse(c.Proxy)
	if err != nil || proxy.Hostname() == "" || strings.HasSuffix(proxy.Host, ":") {
		return nil, errors.New("proxy must be a valid URL with a host and an optional port")
	}
	if strings.HasPrefix(proxy.Host, "[") {
		addr, err := netip.ParseAddr(proxy.Hostname())
		if err != nil || !addr.Is6() {
			return nil, errors.New("proxy has an invalid IPv6 address")
		}
	} else if strings.Contains(proxy.Hostname(), ":") {
		return nil, errors.New("proxy IPv6 address must be enclosed in brackets")
	}
	switch proxy.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, errors.New("proxy scheme must be http, https, socks5, or socks5h")
	}
	if port := proxy.Port(); port != "" {
		if n, err := strconv.ParseUint(port, 10, 16); err != nil || n == 0 {
			return nil, errors.New("proxy port must be between 1 and 65535")
		}
	}
	if (proxy.Path != "" && proxy.Path != "/") || proxy.RawQuery != "" || proxy.ForceQuery || proxy.Fragment != "" {
		return nil, errors.New("proxy URL must not include a path, query, or fragment")
	}
	return proxy, nil
}
