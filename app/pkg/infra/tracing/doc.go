// Package tracing carries the request, correlation and trace identifiers of a request through its context, and between
// services in gRPC metadata.
//
// It is not an OpenTelemetry wrapper: spans, exporters and propagation live in app/pkg/infra/telemetry. GetTraceID
// returns the W3C trace ID of the OpenTelemetry span in the context, so the logs, the spans and the outgoing calls all
// show the same ID; the KSUID-style ID that NewContext generates is only the fallback for a context without a span.
package tracing
