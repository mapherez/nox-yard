package jobs

import (
	"context"
	"github.com/mapherez/nox-yard/internal/store"
)

type jobKey struct{}

func WithJob(ctx context.Context, job store.Job) context.Context {
	return context.WithValue(ctx, jobKey{}, job)
}
func FromContext(ctx context.Context) (store.Job, bool) {
	job, ok := ctx.Value(jobKey{}).(store.Job)
	return job, ok
}
