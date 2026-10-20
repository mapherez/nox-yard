// Package machineauth carries request authorization across the MCP adapter.
package machineauth

import "context"

type Policy struct{ Write, Secrets bool }
type key struct{}

func WithPolicy(ctx context.Context, policy Policy) context.Context {
	return context.WithValue(ctx, key{}, policy)
}
func FromContext(ctx context.Context) (Policy, bool) {
	p, ok := ctx.Value(key{}).(Policy)
	return p, ok
}
