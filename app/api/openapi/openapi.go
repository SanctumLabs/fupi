// Package openapi embeds the OpenAPI spec generated from the handler annotations (make generate.open.api.docs), so the
// binary serves it without depending on the working directory, which the distroless image does not provide (ADR-0016).
package openapi

import _ "embed"

// Spec is the generated Swagger 2.0 document. Do not edit swagger.json: regenerate it.
//
//go:embed swagger.json
var Spec []byte
