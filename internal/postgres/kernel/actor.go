package kernel

import "context"

type actorKey struct{}

// WithActor names the author of item revisions in ctx, independently of the
// action log's source. A worker can use assistant or system.
func WithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

// ActorFromContext returns the author that ctx names. The default is the
// user.
func ActorFromContext(ctx context.Context) string {
	actor, _ := ctx.Value(actorKey{}).(string)
	switch actor {
	case "user", "secretary", "assistant", "system":
		return actor
	default:
		return "user"
	}
}
