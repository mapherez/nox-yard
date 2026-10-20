package yardrelease

import (
	"context"
	"os"
	"runtime"
	"slices"
	"testing"
	"time"
)

// Opt-in, read-only integration against an explicitly selected public image.
// Normal CI uses deterministic local fixtures and never depends on Sigstore uptime.
func TestPublishedReleaseVerification(t *testing.T) {
	source := os.Getenv("NOX_VERIFY_RELEASE_FIXTURE")
	if source == "" {
		t.Skip("requires an explicit signed release fixture and bundled Cosign")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	candidate, err := Trusted(ctx, source, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("verified index=%s platform=%s image=%s", candidate.IndexDigest, candidate.ManifestDigest, candidate.ImageID)
	for _, flag := range []string{"--certificate-identity", "--certificate-oidc-issuer", "--certificate-github-workflow-sha"} {
		t.Run(flag, func(t *testing.T) {
			err := verify(ctx, candidate, func(ctx context.Context, args []string) ([]byte, error) {
				args = slices.Clone(args)
				if index := slices.Index(args, flag); index >= 0 {
					args[index+1] = "unexpected"
				}
				return runCosign(ctx, args)
			})
			if err == nil {
				t.Fatal("incorrect publisher identity/commit accepted")
			}
		})
	}
}
