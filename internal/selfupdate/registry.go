package selfupdate

import (
	"context"
	"runtime"

	"github.com/mapherez/nox-yard/internal/yardrelease"
)

const imageReference = "ghcr.io/mapherez/nox-yard:latest"

type release = yardrelease.Candidate

func latestRelease(ctx context.Context) (release, error) {
	return yardrelease.Resolve(ctx, imageReference, runtime.GOARCH)
}

func validDigest(value string) bool { return yardrelease.ValidDigest(value) }
