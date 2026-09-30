package postgres

import "context"

type actorKey struct{}

// withActor identifies the author of item revisions, independently of the
// action log's source. Workers can use assistant/system without changing saveAction.
func withActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

func actorFromContext(ctx context.Context) string {
	actor, _ := ctx.Value(actorKey{}).(string)
	switch actor {
	case "user", "secretary", "assistant", "system":
		return actor
	default:
		return "user"
	}
}
