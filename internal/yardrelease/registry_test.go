package yardrelease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func digestOf(value string) string {
	hash := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(hash[:])
}

func registryFixture(t *testing.T, index, platform string) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := ""
		switch r.URL.Path {
		case "/token":
			body = `{"token":"fixture-token"}`
		case "/v2/mapherez/nox-yard/manifests/latest", "/v2/mapherez/nox-yard/manifests/" + digestOf(index):
			body = index
		case "/v2/mapherez/nox-yard/manifests/" + digestOf(platform):
			body = platform
		default:
			t.Fatalf("unexpected registry request: %s", r.URL.Path)
		}
		if r.URL.Path != "/token" && r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Fatal("missing registry authentication")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
}

func platformFixture() string {
	return fmt.Sprintf(`{"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"digest":%q}}`, "sha256:"+strings.Repeat("a", 64))
}
func indexFixture(platform, architecture string) string {
	return fmt.Sprintf(`{"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[{"digest":%q,"platform":{"os":"linux","architecture":%q}},{"digest":%q,"platform":{"os":"unknown","architecture":"unknown"}}]}`, digestOf(platform), architecture, "sha256:"+strings.Repeat("b", 64))
}

func TestResolveBindsIndexPlatformAndImageIdentity(t *testing.T) {
	for _, architecture := range []string{"amd64", "arm64"} {
		t.Run(architecture, func(t *testing.T) {
			platform := platformFixture()
			index := indexFixture(platform, architecture)
			for _, source := range []string{Repository + ":latest", Repository + "@" + digestOf(index)} {
				candidate, err := resolve(context.Background(), registryFixture(t, index, platform), source, architecture)
				if err != nil {
					t.Fatal(err)
				}
				if candidate.IndexDigest != digestOf(index) || candidate.ManifestDigest != digestOf(platform) || candidate.ImageID != "sha256:"+strings.Repeat("a", 64) || candidate.Reference() != Repository+"@"+digestOf(index) {
					t.Fatalf("identity chain lost: %+v", candidate)
				}
			}
		})
	}
}

func TestResolveRejectsChangedContentAndAmbiguousPlatforms(t *testing.T) {
	platform := platformFixture()
	index := indexFixture(platform, "amd64")
	for _, variant := range []string{"platform-content", "index-header", "duplicate-platform", "missing-platform", "single-image", "oversized-index"} {
		t.Run(variant, func(t *testing.T) {
			client := registryFixture(t, index, platform)
			base := client.Transport
			client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				response, err := base.RoundTrip(r)
				if err != nil {
					return nil, err
				}
				if strings.Contains(r.URL.Path, "/manifests/") {
					switch variant {
					case "platform-content":
						if strings.HasSuffix(r.URL.Path, digestOf(platform)) {
							response.Body = io.NopCloser(strings.NewReader(platform + " "))
						}
					case "index-header":
						response.Header.Set("Docker-Content-Digest", "sha256:"+strings.Repeat("f", 64))
					case "duplicate-platform":
						if strings.HasSuffix(r.URL.Path, "latest") {
							var value map[string]any
							_ = json.Unmarshal([]byte(index), &value)
							items := value["manifests"].([]any)
							value["manifests"] = append(items, items[0])
							encoded, _ := json.Marshal(value)
							response.Body = io.NopCloser(strings.NewReader(string(encoded)))
						}
					case "missing-platform":
						if strings.HasSuffix(r.URL.Path, "latest") {
							response.Body = io.NopCloser(strings.NewReader(indexFixture(platform, "arm64")))
						}
					case "single-image":
						response.Body = io.NopCloser(strings.NewReader(platform))
					case "oversized-index":
						response.Body = io.NopCloser(strings.NewReader(strings.Repeat(" ", (1<<20)+1)))
					}
				}
				return response, nil
			})
			if _, err := resolve(context.Background(), client, Repository+":latest", "amd64"); err == nil {
				t.Fatal("untrusted registry content accepted")
			}
		})
	}
}

func TestResolveRejectsUnapprovedSourcesBeforeNetwork(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid source reached network")
		return nil, nil
	})}
	for _, source := range []string{"alpine:latest", Repository + "-fake:latest", Repository + ":latest/../../other", Repository + "@sha256:bad", "https://" + Repository + ":latest"} {
		if _, err := resolve(context.Background(), client, source, "amd64"); err == nil {
			t.Fatal("accepted", source)
		}
	}
}
