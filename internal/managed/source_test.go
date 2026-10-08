package managed

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

func TestLoadSourceAcceptsTextInputs(t *testing.T) {
	for _, input := range []SourceInput{
		{Kind: "paste", YAML: "services:\n  web:\n    image: nginx:alpine\n"},
		{Kind: "upload", Filename: "compose.yaml", YAML: "services:\n  web:\n    image: nginx:alpine\n"},
	} {
		source, err := LoadSource(t.Context(), input)
		if err != nil || source.Kind != input.Kind || source.YAML != input.YAML {
			t.Fatalf("LoadSource(%+v) = %+v, %v", input, source, err)
		}
	}
}

func TestHTTPSIntakeResponseAndCancellation(t *testing.T) {
	for _, scenario := range []struct {
		name, body string
		status     int
		ok         bool
	}{
		{"valid", "services:\n  web:\n    image: alpine:3.23\n", http.StatusOK, true},
		{"not-found", "", http.StatusNotFound, false},
		{"redirect", "", http.StatusFound, false},
		{"too-large", strings.Repeat("x", MaxSourceBytes+1), http.StatusOK, false},
		{"invalid-utf8", string([]byte{0xff}), http.StatusOK, false},
		{"empty", "", http.StatusOK, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if scenario.status == http.StatusFound {
					w.Header().Set("Location", "/redirect-target")
				}
				w.WriteHeader(scenario.status)
				_, _ = w.Write([]byte(scenario.body))
			}))
			defer server.Close()
			u, _ := url.Parse(server.URL + "/compose.yaml")
			source, err := fetchSourceURL(t.Context(), u, sourceURLClient(server.Client().Transport))
			if scenario.ok {
				if err != nil || source.Kind != "url" || source.URL != u.String() || source.Filename != "compose.yaml" || source.YAML != scenario.body {
					t.Fatalf("invalid HTTPS intake: %v", err)
				}
			} else if !errors.Is(err, ErrInvalidSource) {
				t.Fatalf("unsafe response accepted: %v", err)
			}
		})
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("services: {}")) }))
	defer server.Close()
	u, _ := url.Parse(server.URL + "/compose.yml")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := fetchSourceURL(ctx, u, sourceURLClient(server.Client().Transport)); !errors.Is(err, ErrInvalidSource) {
		t.Fatal("cancelled fetch accepted")
	}
}

func TestLoadSourceRejectsInvalidInputsBeforeNetworkAccess(t *testing.T) {
	for _, input := range []SourceInput{
		{Kind: "paste", YAML: "   "},
		{Kind: "paste", YAML: "services:\x00"},
		{Kind: "paste", YAML: string([]byte{0xff})},
		{Kind: "paste", YAML: strings.Repeat("a", MaxSourceBytes+1)},
		{Kind: "upload", Filename: "../compose.yaml", YAML: "services: {}"},
		{Kind: "url", URL: "http://example.com/compose.yaml"},
		{Kind: "url", URL: "https://user:pass@example.com/compose.yaml"},
		{Kind: "url", URL: "https://example.com:8443/compose.yaml"},
		{Kind: "url", URL: "https://example.com/compose.yaml#fragment"},
	} {
		_, err := LoadSource(context.Background(), input)
		if !errors.Is(err, ErrInvalidSource) {
			t.Fatalf("LoadSource(%+v) error = %v, want ErrInvalidSource", input, err)
		}
	}
}

func TestPublicIPBlocksLocalAndSpecialAddresses(t *testing.T) {
	for address, want := range map[string]bool{
		"8.8.8.8":              true,
		"2606:4700:4700::1111": true,
		"127.0.0.1":            false,
		"10.0.0.1":             false,
		"100.64.0.1":           false,
		"169.254.169.254":      false,
		"172.16.0.1":           false,
		"192.168.1.1":          false,
		"198.18.0.1":           false,
		"203.0.113.1":          false,
		"::ffff:127.0.0.1":     false,
		"::1":                  false,
		"fc00::1":              false,
		"fe80::1":              false,
		"2001:db8::1":          false,
		"2002:c0a8:101::1":     false,
	} {
		if got := publicIP(netip.MustParseAddr(address)); got != want {
			t.Errorf("publicIP(%s) = %v, want %v", address, got, want)
		}
	}
}
