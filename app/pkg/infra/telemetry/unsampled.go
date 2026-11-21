package telemetry

import (
	"context"
	"encoding/binary"
	"math/rand/v2"

	"go.opentelemetry.io/otel/trace"
)

// Unsampled returns ctx carrying a parent span that is valid but not sampled. A parent-based sampler, the SDK default,
// records nothing beneath such a parent, so work done under it (the readiness checks' Postgres and Redis pings) leaves
// no spans and does not start a new trace on every probe. If ctx already holds a span its IDs are kept and only the
// sampled flag is cleared. With a plain always_on sampler this has no effect. When ctx holds no span the IDs are made up
// and belong to no trace, so the logger leaves them out of the lines written under that context.
func Unsampled(ctx context.Context) context.Context {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		sc = trace.NewSpanContext(trace.SpanContextConfig{TraceID: randomTraceID(), SpanID: randomSpanID()})
		ctx = context.WithValue(ctx, syntheticKey{}, true)
	}
	return trace.ContextWithSpanContext(ctx, sc.WithTraceFlags(sc.TraceFlags()&^trace.FlagsSampled))
}

// syntheticKey marks a context whose span IDs Unsampled made up.
type syntheticKey struct{}

func isSynthetic(ctx context.Context) bool {
	synthetic, _ := ctx.Value(syntheticKey{}).(bool)
	return synthetic
}

// The IDs only have to be valid (non-zero); nothing is exported under them.
func randomTraceID() (id trace.TraceID) {
	binary.BigEndian.PutUint64(id[:8], rand.Uint64()|1)
	binary.BigEndian.PutUint64(id[8:], rand.Uint64())
	return id
}

func randomSpanID() (id trace.SpanID) {
	binary.BigEndian.PutUint64(id[:], rand.Uint64()|1)
	return id
}
