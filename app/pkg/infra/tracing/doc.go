// Package tracing provides a wrapper around OpenTelemetry to simplify
// instrumentation of Go applications. It provides a set of utilities to
// create and manage spans, as well as to handle context propagation.
//
// This package is designed to be used in conjunction with the OpenTelemetry
// Go SDK and the OpenTelemetry Collector. It provides a simple and consistent
// interface for instrumenting your code, making it easier to add tracing
// capabilities to your applications.
//
// The package includes functions for creating spans, adding attributes and
// events to spans, and managing the lifecycle of spans. It also provides
// utilities for propagating context across process boundaries, allowing you
// to trace requests as they flow through your system.
//
// The package is designed to be easy to use and integrate into existing
// applications. It provides a set of default configurations and best
// practices for instrumenting your code, while still allowing for
// customization and flexibility.
//
// The package is intended for use in production environments, and has been
// tested for performance and reliability. It is designed to work with
// distributed systems and microservices architectures, making it a
// valuable tool for modern application development.
//
// Example usage:
//
//	import (
//		"context"
//		"log/slog"
//		"github.com/sanctumlabs/curtz/app/pkg/infra/tracing"
//			"go.opentelemetry.io/otel"
//			"go.opentelemetry.io/otel/trace"
//	)
//
//	func main() {
//		ctx := context.Background()
//		tracer := tracing.NewTracer("example-service")
//		ctx, span := tracer.Start(ctx, "example-operation")
//		defer span.End()
//
//		// Do some work...
//		// Add attributes to the span
//		span.SetAttributes("key", "value")
//		// Add an event to the span
//		span.AddEvent("event-name", trace.WithAttributes("key", "value"))
//
//		// End the span
//		span.End()
//	}
//
//	// Output:
//	// example-operation
//	//   key: value
//	//   event-name: key=value
//	//   ...
//	//   ...
//	//   example-operation ended
//	//   ...
//	//   ...
//	//   example-operation ended
//	//   ...
//	//   ...
//	//   example-operation ended
//	//   ...
//	//   ...
package tracing
