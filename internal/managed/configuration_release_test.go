package managed

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/mapherez/nox-yard/internal/imageidentity"
	"github.com/mapherez/nox-yard/internal/yardrelease"
)

func signedFileFixture(source string) (Preview, *fileDeployment, yardrelease.Candidate) {
	resolved, _ := json.Marshal(map[string]any{"services": map[string]any{"nox-yard": map[string]any{"image": source, "pull_policy": "never", "environment": map[string]string{"PRIVATE": "unchanged"}}, "other": map[string]string{"image": "alpine:3.23"}}})
	work := &fileDeployment{Self: true, Source: FileSnapshot{Resolved: resolved}}
	preview := filePreview(work.Source)
	candidate := yardrelease.Candidate{Source: source, IndexDigest: "sha256:" + strings.Repeat("a", 64), ManifestDigest: "sha256:" + strings.Repeat("b", 64), ImageID: "sha256:" + strings.Repeat("c", 64), Architecture: runtime.GOARCH}
	return preview, work, candidate
}

func TestSignedYardUpdatePinsPullAndCreationWithoutChangingHostSource(t *testing.T) {
	for _, source := range []string{yardrelease.Repository + ":latest", yardrelease.Repository + ":v1.3.2", yardrelease.Repository + "@sha256:" + strings.Repeat("a", 64)} {
		t.Run(source, func(t *testing.T) {
			preview, work, candidate := signedFileFixture(source)
			original := string(work.Source.Resolved)
			manager := &Manager{verifyYardRelease: func(_ context.Context, reference, arch string) (yardrelease.Candidate, error) {
				if reference != source || arch != runtime.GOARCH {
					t.Fatal("source pin ignored")
				}
				return candidate, nil
			}}
			if err := manager.prepareSignedYardUpdate(context.Background(), work, preview); err != nil {
				t.Fatal(err)
			}
			// The job survives transport to a network-isolated independent worker.
			encoded, _ := json.Marshal(work)
			work = &fileDeployment{}
			if err := json.Unmarshal(encoded, work); err != nil {
				t.Fatal(err)
			}
			definition, err := verifiedYardPull(work, preview)
			if err != nil {
				t.Fatal(err)
			}
			var model struct {
				Services map[string]map[string]any `json:"services"`
			}
			if err := json.Unmarshal([]byte(definition), &model); err != nil {
				t.Fatal(err)
			}
			if model.Services["nox-yard"]["image"] != candidate.Reference() || model.Services["nox-yard"]["pull_policy"] != nil || model.Services["other"]["image"] != "alpine:3.23" {
				t.Fatal("pull not pinned or unrelated service changed", definition)
			}
			if string(work.Source.Resolved) != original || preview.model.Services["nox-yard"].Image != source {
				t.Fatal("host/source configuration changed")
			}
			manager.imageDefaults = func(_ context.Context, reference string) (imageDefaults, error) {
				if reference == source && reference != candidate.Reference() {
					t.Fatal("mutable tag was inspected after verification")
				}
				if reference == candidate.Reference() {
					return imageDefaults{ID: candidate.ImageID, Platform: "linux/" + runtime.GOARCH}, nil
				}
				return imageDefaults{ID: "other-image"}, nil
			}
			defaults, err := manager.fileImageDefaults(context.Background(), preview, work.VerifiedYard)
			if err != nil {
				t.Fatal(err)
			}
			_, targets := imageTargets(preview, defaults)
			created, err := resolvedDefinition(preview, "/projects/yard", targets)
			if err != nil {
				t.Fatal(err)
			}
			_ = json.Unmarshal([]byte(created), &model)
			yard := model.Services["nox-yard"]
			if yard["image"] != candidate.ImageID || yard["labels"].(map[string]any)[imageidentity.ReferenceLabel] != source || yard["pull_policy"] != "never" {
				t.Fatal("creation lost verified ID or original reference", created)
			}
		})
	}
}

func TestSignedYardUpdateFailsBeforePreparingPull(t *testing.T) {
	preview, work, _ := signedFileFixture(yardrelease.Repository + ":latest")
	failure := errors.New("signature verification failed")
	manager := &Manager{verifyYardRelease: func(context.Context, string, string) (yardrelease.Candidate, error) {
		return yardrelease.Candidate{}, failure
	}}
	err := manager.prepareSignedYardUpdate(context.Background(), work, preview)
	if !errors.Is(err, failure) || work.VerifiedYard != nil || work.Mutation {
		t.Fatal("failed verification prepared an update", err)
	}
	if definition, err := verifiedYardPull(work, preview); err == nil || definition != "" {
		t.Fatal("worker accepted a job without a verified candidate")
	}
}

func TestOfflineWorkerRejectsMismatchedVerifiedCandidate(t *testing.T) {
	for _, mismatch := range []string{"source", "architecture", "digest"} {
		t.Run(mismatch, func(t *testing.T) {
			preview, work, candidate := signedFileFixture(yardrelease.Repository + ":latest")
			switch mismatch {
			case "source":
				candidate.Source = yardrelease.Repository + ":v1.3.2"
			case "architecture":
				candidate.Architecture = "other"
			case "digest":
				candidate.IndexDigest = "invalid"
			}
			work.VerifiedYard = &candidate
			if _, err := verifiedYardPull(work, preview); err == nil {
				t.Fatal("worker accepted mismatched verification")
			}
		})
	}
}

func TestSignedYardUpdateRejectsDifferentPulledImage(t *testing.T) {
	preview, _, candidate := signedFileFixture(yardrelease.Repository + ":latest")
	manager := &Manager{imageDefaults: func(context.Context, string) (imageDefaults, error) {
		return imageDefaults{ID: "unverified-image"}, nil
	}}
	if _, err := manager.fileImageDefaults(context.Background(), preview, &candidate); err == nil {
		t.Fatal("different pulled image accepted")
	}
}

func TestSignedYardUpdateSupportsClassicAndContainerdImageIDs(t *testing.T) {
	preview, _, candidate := signedFileFixture(yardrelease.Repository + ":latest")
	for _, id := range []string{candidate.ImageID, candidate.ManifestDigest, candidate.IndexDigest} {
		manager := &Manager{imageDefaults: func(_ context.Context, reference string) (imageDefaults, error) {
			if reference == candidate.Reference() {
				return imageDefaults{ID: id, Platform: "linux/" + runtime.GOARCH}, nil
			}
			return imageDefaults{ID: "other-image"}, nil
		}}
		defaults, err := manager.fileImageDefaults(context.Background(), preview, &candidate)
		if err != nil || defaults[candidate.Source].ID != id {
			t.Fatal("verified Docker identity rejected", id, err)
		}
	}
	manager := &Manager{imageDefaults: func(context.Context, string) (imageDefaults, error) {
		return imageDefaults{ID: candidate.IndexDigest, Platform: "linux/other"}, nil
	}}
	if _, err := manager.fileImageDefaults(context.Background(), preview, &candidate); err == nil {
		t.Fatal("signed index accepted wrong platform")
	}
}
