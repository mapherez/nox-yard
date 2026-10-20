package yardrelease

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"
)

func testCandidate() Candidate {
	return Candidate{Source: Repository + ":latest", IndexDigest: "sha256:" + strings.Repeat("a", 64), ManifestDigest: "sha256:" + strings.Repeat("b", 64), ImageID: "sha256:" + strings.Repeat("c", 64), Architecture: "amd64"}
}
func signatureJSON(digest, version, revision string) []byte {
	value := []any{map[string]any{"critical": map[string]any{"image": map[string]string{"docker-manifest-digest": digest}}, "optional": map[string]string{"release": version, "revision": revision}}}
	encoded, _ := json.Marshal(value)
	return encoded
}

func TestVerifyRequiresExactPublisherAndImmutableDigest(t *testing.T) {
	candidate := testCandidate()
	err := verify(context.Background(), candidate, func(ctx context.Context, args []string) ([]byte, error) {
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 2*time.Minute {
			t.Fatal("verification lacks bounded deadline")
		}
		for flag, value := range map[string]string{"--certificate-identity": Signer, "--certificate-oidc-issuer": Issuer, "--certificate-github-workflow-repository": "mapherez/nox-yard", "--certificate-github-workflow-trigger": "push"} {
			i := slices.Index(args, flag)
			if i < 0 || args[i+1] != value {
				t.Fatal("weakened verification policy", args)
			}
		}
		if args[len(args)-1] != candidate.Reference() || strings.Contains(strings.Join(args, " "), "insecure") {
			t.Fatal("mutable or unsafe verification", args)
		}
		return signatureJSON(candidate.IndexDigest, "v1.3.2", strings.Repeat("d", 40)), nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestVerifyFailsClosed(t *testing.T) {
	candidate := testCandidate()
	for name, output := range map[string][]byte{
		"no signatures": []byte(`[]`), "invalid JSON": []byte(`invalid`), "wrong digest": signatureJSON(candidate.ManifestDigest, "v1.3.2", strings.Repeat("d", 40)), "missing version": signatureJSON(candidate.IndexDigest, "", strings.Repeat("d", 40)), "invalid commit": signatureJSON(candidate.IndexDigest, "v1.3.2", strings.Repeat("z", 40)), "prerelease latest": signatureJSON(candidate.IndexDigest, "v1.3.3-rc.1", strings.Repeat("d", 40)), "oversized": []byte(strings.Repeat("x", (1<<20)+1)),
	} {
		t.Run(name, func(t *testing.T) {
			if err := verify(context.Background(), candidate, func(context.Context, []string) ([]byte, error) { return output, nil }); err == nil {
				t.Fatal("invalid verification accepted")
			}
		})
	}
	for _, failure := range []error{errors.New("unsigned image"), errors.New("verifier missing"), errors.New("private stderr with API key")} {
		err := verify(context.Background(), candidate, func(context.Context, []string) ([]byte, error) { return nil, failure })
		if err == nil || strings.Contains(err.Error(), "API key") {
			t.Fatal("verification failed open or leaked diagnostics", err)
		}
	}
}

func TestVerifyPreservesVersionPinsAndCancellation(t *testing.T) {
	candidate := testCandidate()
	candidate.Source = Repository + ":v1.3.2"
	run := func(context.Context, []string) ([]byte, error) {
		return signatureJSON(candidate.IndexDigest, "v1.3.3", strings.Repeat("d", 40)), nil
	}
	if err := verify(context.Background(), candidate, run); err == nil {
		t.Fatal("version tag accepted another signed release")
	}
	candidate.Source = Repository + "@" + candidate.ManifestDigest
	if err := verify(context.Background(), candidate, run); err == nil {
		t.Fatal("digest pin changed")
	}
	candidate = testCandidate()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := verify(ctx, candidate, func(ctx context.Context, _ []string) ([]byte, error) { return nil, ctx.Err() })
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
}

func TestVerifierOutputIsBoundedEvenThroughIOCopy(t *testing.T) {
	var output boundedOutput
	if _, err := io.Copy(&output, strings.NewReader(strings.Repeat("x", (1<<20)+1))); err == nil {
		t.Fatal("output limit bypassed")
	}
	if output.buffer.Len() > 1<<20 {
		t.Fatal("unbounded verifier output")
	}
}

func TestCertificateCommitMismatchBlocksOtherwiseValidSignature(t *testing.T) {
	candidate := testCandidate()
	revision := strings.Repeat("d", 40)
	calls := 0
	err := verify(context.Background(), candidate, func(_ context.Context, args []string) ([]byte, error) {
		calls++
		if calls == 1 {
			return signatureJSON(candidate.IndexDigest, "v1.3.2", revision), nil
		}
		index := slices.Index(args, "--certificate-github-workflow-sha")
		if index < 0 || args[index+1] != revision || !slices.Contains(args, "revision="+revision) || !slices.Contains(args, "release=v1.3.2") {
			t.Fatal("signed annotations were not bound to the workflow commit", args)
		}
		return nil, errors.New("certificate commit differs")
	})
	if err == nil || calls != 2 {
		t.Fatal("certificate commit mismatch accepted", calls, err)
	}
}

func TestDockerIDsMustBelongToVerifiedIndexChain(t *testing.T) {
	candidate := testCandidate()
	for _, id := range []string{candidate.ImageID, candidate.ManifestDigest, candidate.IndexDigest} {
		if !candidate.MatchesImageID(id) {
			t.Fatal("verified Docker identity rejected", id)
		}
	}
	for _, id := range []string{"", "sha256:invalid", "sha256:" + strings.Repeat("f", 64)} {
		if candidate.MatchesImageID(id) {
			t.Fatal("unverified Docker identity accepted", id)
		}
	}
}
