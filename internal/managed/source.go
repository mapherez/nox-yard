package managed

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxSourceBytes = 1024 * 1024

var ErrInvalidSource = errors.New("invalid Compose source")

type SourceInput struct {
	Kind     string `json:"kind"`
	URL      string `json:"url,omitempty"`
	Filename string `json:"filename,omitempty"`
	YAML     string `json:"yaml,omitempty"`
}

type Source struct {
	Kind     string
	URL      string
	Filename string
	YAML     string
}

// LoadSource reads a Compose source without evaluating it or changing the host.
// URL imports use their own transport so proxy settings cannot bypass the
// public-address check.
func LoadSource(ctx context.Context, input SourceInput) (Source, error) {
	switch input.Kind {
	case "paste":
		if input.URL != "" || input.Filename != "" {
			return Source{}, fmt.Errorf("%w: pasted source cannot include a URL or filename", ErrInvalidSource)
		}
		return textSource(input.Kind, "", "", input.YAML)
	case "upload":
		if input.URL != "" {
			return Source{}, fmt.Errorf("%w: uploaded source cannot include a URL", ErrInvalidSource)
		}
		name := path.Base(strings.ReplaceAll(input.Filename, "\\", "/"))
		if name == "." || name == "/" || name == "" || name != input.Filename {
			return Source{}, fmt.Errorf("%w: choose a Compose file", ErrInvalidSource)
		}
		return textSource(input.Kind, "", name, input.YAML)
	case "url":
		if input.YAML != "" || input.Filename != "" {
			return Source{}, fmt.Errorf("%w: URL sources cannot include file content", ErrInvalidSource)
		}
		return loadURL(ctx, input.URL)
	default:
		return Source{}, fmt.Errorf("%w: choose URL, paste, or upload", ErrInvalidSource)
	}
}

func textSource(kind, sourceURL, filename, content string) (Source, error) {
	if len(content) > MaxSourceBytes {
		return Source{}, fmt.Errorf("%w: Compose file exceeds 1 MiB", ErrInvalidSource)
	}
	if !utf8.ValidString(content) || strings.ContainsRune(content, 0) || strings.TrimSpace(content) == "" {
		return Source{}, fmt.Errorf("%w: Compose file must contain UTF-8 text", ErrInvalidSource)
	}
	return Source{Kind: kind, URL: sourceURL, Filename: filename, YAML: content}, nil
}

func loadURL(ctx context.Context, raw string) (Source, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return Source{}, fmt.Errorf("%w: enter a public HTTPS URL", ErrInvalidSource)
	}
	if port := u.Port(); port != "" && port != "443" {
		return Source{}, fmt.Errorf("%w: HTTPS URL must use port 443", ErrInvalidSource)
	}
	if len(raw) > 2048 {
		return Source{}, fmt.Errorf("%w: URL is too long", ErrInvalidSource)
	}
	transport := &http.Transport{
		Proxy:                  nil,
		DisableKeepAlives:      true,
		ResponseHeaderTimeout:  5 * time.Second,
		MaxResponseHeaderBytes: 32 * 1024,
		DialContext:            dialPublic,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirects are not allowed for Compose URLs")
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Source{}, fmt.Errorf("%w: invalid URL", ErrInvalidSource)
	}
	response, err := client.Do(request)
	if err != nil {
		return Source{}, fmt.Errorf("%w: cannot fetch public HTTPS URL", ErrInvalidSource)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Source{}, fmt.Errorf("%w: URL returned HTTP %d", ErrInvalidSource, response.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, MaxSourceBytes+1))
	if err != nil {
		return Source{}, fmt.Errorf("%w: cannot read URL: %v", ErrInvalidSource, err)
	}
	return textSource("url", u.String(), path.Base(u.Path), string(content))
}

func dialPublic(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port != "443" {
		return nil, fmt.Errorf("invalid HTTPS destination")
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, fmt.Errorf("cannot resolve HTTPS destination")
	}
	for _, address := range addresses {
		if !publicIP(address) {
			return nil, fmt.Errorf("HTTPS destination is not public")
		}
	}
	var dialer net.Dialer
	for _, address := range addresses {
		connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(address.String(), port))
		if err == nil {
			return connection, nil
		}
	}
	return nil, fmt.Errorf("cannot connect to HTTPS destination")
}

var nonPublicIPv4 = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
}

func publicIP(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() {
		return false
	}
	if address.Is4() {
		for _, prefix := range nonPublicIPv4 {
			if prefix.Contains(address) {
				return false
			}
		}
		return true
	}
	return netip.MustParsePrefix("2000::/3").Contains(address) &&
		!netip.MustParsePrefix("2001::/23").Contains(address) &&
		!netip.MustParsePrefix("2001:db8::/32").Contains(address) &&
		!netip.MustParsePrefix("2002::/16").Contains(address) &&
		!netip.MustParsePrefix("3fff::/20").Contains(address)
}
