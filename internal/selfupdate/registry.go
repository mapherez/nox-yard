package selfupdate

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"
)

const imageReference = "ghcr.io/mapherez/nox-yard:latest"

const manifestAccept = "application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json"

type release struct {
	ManifestDigest string
	ImageID        string
}

type registryManifest struct {
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

func latestRelease(ctx context.Context) (release, error) {
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if req.URL.Scheme != "https" || req.URL.Host != "ghcr.io" {
				return errors.New("unexpected registry redirect")
			}
			return nil
		},
	}
	tokenURL := "https://ghcr.io/token?service=ghcr.io&scope=" + url.QueryEscape("repository:mapherez/nox-yard:pull")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	if err != nil {
		return release{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return release{}, fmt.Errorf("request GHCR token: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return release{}, fmt.Errorf("GHCR token request returned HTTP %d; the image package must be public", response.StatusCode)
	}
	var token struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&token); err != nil || token.Token == "" {
		return release{}, errors.New("GHCR did not return an access token")
	}

	manifest, err := getManifest(ctx, client, token.Token, "latest")
	if err != nil {
		return release{}, err
	}
	if len(manifest.Manifests) == 0 {
		return release{}, errors.New("GHCR latest is not a multi-platform image")
	}
	for _, candidate := range manifest.Manifests {
		if candidate.Platform.OS != "linux" || candidate.Platform.Architecture != runtime.GOARCH {
			continue
		}
		if !validDigest(candidate.Digest) {
			return release{}, errors.New("GHCR returned an invalid platform digest")
		}
		platformManifest, err := getManifest(ctx, client, token.Token, candidate.Digest)
		if err != nil {
			return release{}, err
		}
		if !validDigest(platformManifest.Config.Digest) {
			return release{}, errors.New("GHCR returned an invalid image ID")
		}
		return release{ManifestDigest: candidate.Digest, ImageID: platformManifest.Config.Digest}, nil
	}
	return release{}, fmt.Errorf("GHCR latest has no linux/%s image", runtime.GOARCH)
}

func getManifest(ctx context.Context, client *http.Client, token, reference string) (registryManifest, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://ghcr.io/v2/mapherez/nox-yard/manifests/"+reference, nil)
	if err != nil {
		return registryManifest{}, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", manifestAccept)
	response, err := client.Do(request)
	if err != nil {
		return registryManifest{}, fmt.Errorf("read GHCR manifest: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return registryManifest{}, fmt.Errorf("GHCR manifest returned HTTP %d", response.StatusCode)
	}
	var manifest registryManifest
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&manifest); err != nil {
		return registryManifest{}, fmt.Errorf("decode GHCR manifest: %w", err)
	}
	return manifest, nil
}

func validDigest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}
