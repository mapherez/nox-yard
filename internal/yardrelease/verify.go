package yardrelease

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const Signer = "https://github.com/mapherez/nox-yard/.github/workflows/release.yml@refs/heads/master"
const Issuer = "https://token.actions.githubusercontent.com"

// Trusted resolves once and verifies the index before returning its immutable
// reference and the image ID bound to that index's platform manifest.
func Trusted(ctx context.Context, reference, architecture string) (Candidate, error) {
	candidate, err := Resolve(ctx, reference, architecture)
	if err == nil {
		err = Verify(ctx, candidate)
	}
	return candidate, err
}

func Verify(ctx context.Context, candidate Candidate) error { return verify(ctx, candidate, runCosign) }

func verify(ctx context.Context, candidate Candidate, run func(context.Context, []string) ([]byte, error)) error {
	selector, err := imageSelector(candidate.Source)
	if err != nil {
		return err
	}
	if !ValidDigest(candidate.IndexDigest) || !ValidDigest(candidate.ManifestDigest) || !ValidDigest(candidate.ImageID) || (ValidDigest(selector) && selector != candidate.IndexDigest) {
		return errors.New("Yard update blocked: invalid release identity")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	args := []string{"verify", "--certificate-identity", Signer, "--certificate-oidc-issuer", Issuer, "--certificate-github-workflow-repository", "mapherez/nox-yard", "--certificate-github-workflow-trigger", "push", "--output", "json", candidate.Reference()}
	output, err := run(ctx, args)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("Yard update blocked: signature verification did not complete: %w", ctx.Err())
		}
		return errors.New("Yard update blocked: signature verification failed or is unavailable. Use an official signed release and check access to GHCR and Sigstore")
	}
	var signatures []struct {
		Critical struct {
			Image struct {
				Digest string `json:"docker-manifest-digest"`
			} `json:"image"`
		} `json:"critical"`
		Optional struct {
			Release  string `json:"release"`
			Revision string `json:"revision"`
		} `json:"optional"`
	}
	if len(output) > 1<<20 || json.Unmarshal(output, &signatures) != nil {
		return errors.New("Yard update blocked: signature verification returned an invalid result")
	}
	for _, signature := range signatures {
		version, revision := signature.Optional.Release, signature.Optional.Revision
		if signature.Critical.Image.Digest != candidate.IndexDigest || !versionTag.MatchString(version) || len(revision) != 40 || revision != strings.ToLower(revision) {
			continue
		}
		if _, err := hex.DecodeString(revision); err != nil {
			continue
		}
		if (selector == "latest" && strings.Contains(version, "-")) || (versionTag.MatchString(selector) && selector != version) {
			continue
		}
		// Bind the signed revision to the certificate's actual workflow commit,
		// rather than accepting a well-formed annotation on its own.
		checks := append([]string(nil), args[:len(args)-1]...)
		checks = append(checks, "--certificate-github-workflow-sha", revision, "-a", "revision="+revision, "-a", "release="+version, candidate.Reference())
		if _, err := run(ctx, checks); err != nil {
			return errors.New("Yard update blocked: signed source revision does not match the authorized workflow, or verification could not complete")
		}
		return nil
	}
	return errors.New("Yard update blocked: verified signature does not match the release digest, version or source revision")
}

type boundedOutput struct{ buffer bytes.Buffer }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.buffer.Len()+len(p) > 1<<20 {
		return 0, errors.New("signature verification output exceeds limit")
	}
	return b.buffer.Write(p)
}

func runCosign(ctx context.Context, args []string) ([]byte, error) {
	command := exec.CommandContext(ctx, "/usr/local/bin/cosign", args...)
	command.Env = []string{}
	// Do not inherit COSIGN/SIGSTORE overrides, credentials or Yard API keys.
	for _, name := range []string{"PATH", "HOME", "TMPDIR", "SSL_CERT_FILE", "SSL_CERT_DIR", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"} {
		if value, ok := os.LookupEnv(name); ok {
			command.Env = append(command.Env, name+"="+value)
		}
	}
	command.WaitDelay = 5 * time.Second
	var output boundedOutput
	command.Stdout, command.Stderr = &output, io.Discard
	if err := command.Run(); err != nil {
		return nil, err
	}
	return output.buffer.Bytes(), nil
}
