// Package yardrelease resolves and verifies official Yard release images.
package yardrelease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const Repository = "ghcr.io/mapherez/nox-yard"
const manifestAccept = "application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json"

var versionTag = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$`)

type Candidate struct {
	Source         string
	IndexDigest    string
	ManifestDigest string
	ImageID        string
	Architecture   string
}

func (c Candidate) Reference() string { return Repository + "@" + c.IndexDigest }

// Classic Docker uses the config digest as image ID; the containerd image store
// can use an index or platform-manifest digest. All three are bound by the
// verified index. Installers must also fix/check the selected Linux platform.
func (c Candidate) MatchesImageID(id string) bool {
	return ValidDigest(id) && (id == c.ImageID || id == c.ManifestDigest || id == c.IndexDigest)
}

func ValidDigest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != 71 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value[7:])
	return err == nil
}

func imageSelector(reference string) (string, error) {
	if strings.HasPrefix(reference, Repository+"@") {
		selector := strings.TrimPrefix(reference, Repository+"@")
		if ValidDigest(selector) {
			return selector, nil
		}
	} else if strings.HasPrefix(reference, Repository+":") {
		selector := strings.TrimPrefix(reference, Repository+":")
		if selector == "latest" || versionTag.MatchString(selector) {
			return selector, nil
		}
	}
	return "", errors.New("Yard update blocked: use an official ghcr.io/mapherez/nox-yard release tag or index digest")
}

type manifest struct {
	MediaType string `json:"mediaType"`
	Config    struct {
		Digest string `json:"digest"`
	} `json:"config"`
	Manifests []struct {
		Digest   string `json:"digest"`
		Platform struct {
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
		} `json:"platform"`
	} `json:"manifests"`
}

func Resolve(ctx context.Context, reference, architecture string) (Candidate, error) {
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || req.URL.Host != "ghcr.io" || len(via) >= 5 {
			return errors.New("unexpected registry redirect")
		}
		return nil
	}}
	return resolve(ctx, client, reference, architecture)
}

func resolve(ctx context.Context, client *http.Client, reference, architecture string) (Candidate, error) {
	selector, err := imageSelector(reference)
	if err != nil {
		return Candidate{}, err
	}
	if architecture != "amd64" && architecture != "arm64" {
		return Candidate{}, errors.New("Yard update blocked: unsupported Linux architecture")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://ghcr.io/token?service=ghcr.io&scope="+url.QueryEscape("repository:mapherez/nox-yard:pull"), nil)
	if err != nil {
		return Candidate{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return Candidate{}, fmt.Errorf("request GHCR token: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Candidate{}, fmt.Errorf("GHCR token request returned HTTP %d; the image package must be public", response.StatusCode)
	}
	var token struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&token); err != nil || token.Token == "" {
		return Candidate{}, errors.New("GHCR did not return an access token")
	}
	index, indexDigest, err := readManifest(ctx, client, token.Token, selector)
	if err != nil {
		return Candidate{}, err
	}
	if index.MediaType != "application/vnd.oci.image.index.v1+json" && index.MediaType != "application/vnd.docker.distribution.manifest.list.v2+json" {
		return Candidate{}, errors.New("Yard update blocked: release is not a signed multi-platform index")
	}
	platformDigest := ""
	for _, item := range index.Manifests {
		if item.Platform.OS != "linux" || item.Platform.Architecture != architecture {
			continue
		}
		if platformDigest != "" || !ValidDigest(item.Digest) {
			return Candidate{}, errors.New("GHCR returned an ambiguous or invalid platform digest")
		}
		platformDigest = item.Digest
	}
	if platformDigest == "" {
		return Candidate{}, fmt.Errorf("GHCR release has no linux/%s image", architecture)
	}
	platform, _, err := readManifest(ctx, client, token.Token, platformDigest)
	if err != nil {
		return Candidate{}, err
	}
	if !ValidDigest(platform.Config.Digest) {
		return Candidate{}, errors.New("GHCR returned an invalid image ID")
	}
	return Candidate{Source: reference, IndexDigest: indexDigest, ManifestDigest: platformDigest, ImageID: platform.Config.Digest, Architecture: architecture}, nil
}

func readManifest(ctx context.Context, client *http.Client, token, selector string) (manifest, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://ghcr.io/v2/mapherez/nox-yard/manifests/"+selector, nil)
	if err != nil {
		return manifest{}, "", err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", manifestAccept)
	response, err := client.Do(request)
	if err != nil {
		return manifest{}, "", fmt.Errorf("read GHCR manifest: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return manifest{}, "", fmt.Errorf("GHCR manifest returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return manifest{}, "", errors.New("GHCR manifest is unreadable or too large")
	}
	hash := sha256.Sum256(body)
	digest := "sha256:" + hex.EncodeToString(hash[:])
	if (ValidDigest(selector) && selector != digest) || (response.Header.Get("Docker-Content-Digest") != "" && response.Header.Get("Docker-Content-Digest") != digest) {
		return manifest{}, "", errors.New("Yard update blocked: registry manifest digest does not match its content")
	}
	var value manifest
	if err := json.Unmarshal(body, &value); err != nil {
		return manifest{}, "", fmt.Errorf("decode GHCR manifest: %w", err)
	}
	return value, digest, nil
}
