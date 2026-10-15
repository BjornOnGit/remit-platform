// Package corr carries the request correlation id through a context.
package corr

import "context"

type key struct{}

func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, key{}, id)
}

// From returns the correlation id, or "" if none is set.
func From(ctx context.Context) string {
	id, _ := ctx.Value(key{}).(string)
	return id
}