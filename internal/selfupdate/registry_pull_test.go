package selfupdate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/mapherez/nox-yard/internal/yardrelease"
	"github.com/moby/moby/client"
)

func TestFallbackPullUsesVerifiedDigestAndRejectsDifferentImage(t *testing.T) {
	for _, variant := range []string{"config", "manifest", "index", "mismatch"} {
		t.Run(variant, func(t *testing.T) {
			mismatch := variant == "mismatch"
			candidate := yardrelease.Candidate{IndexDigest: "sha256:" + strings.Repeat("a", 64), ImageID: "sha256:" + strings.Repeat("b", 64), ManifestDigest: "sha256:" + strings.Repeat("d", 64)}
			pulled, inspected := false, false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "/images/create"):
					pulled = true
					if !strings.Contains(r.URL.RawQuery, strings.Repeat("a", 64)) || strings.Contains(r.URL.RawQuery, "latest") {
						t.Error("pull used a mutable source", r.URL.RawQuery)
					}
					_, _ = w.Write([]byte(`{"status":"Download complete"}` + "\n"))
				case strings.HasSuffix(r.URL.Path, "/json"):
					inspected = true
					if !strings.Contains(r.URL.Path, candidate.IndexDigest) {
						t.Error("inspect used another reference", r.URL.Path)
					}
					image := candidate.ImageID
					if variant == "manifest" {
						image = candidate.ManifestDigest
					}
					if variant == "index" {
						image = candidate.IndexDigest
					}
					if mismatch {
						image = "sha256:" + strings.Repeat("c", 64)
					}
					_ = json.NewEncoder(w).Encode(map[string]string{"Id": image, "Os": "linux", "Architecture": runtime.GOARCH})
				default:
					t.Error("unexpected mutation", r.Method, r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			cli, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.52"))
			if err != nil {
				t.Fatal(err)
			}
			defer cli.Close()
			_, err = pullAndVerify(context.Background(), cli, candidate)
			if !pulled || !inspected || (err != nil) != mismatch {
				t.Fatal("incorrect pull/verification outcome", err)
			}
		})
	}
}
