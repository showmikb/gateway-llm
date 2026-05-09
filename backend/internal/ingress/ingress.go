// Package ingress holds wire-format adapters. Each subpackage translates
// one non-OpenAI SDK's request shape into the canonical types.ChatCompletionRequest
// (which is itself a thin wrapper around ir.ChatRequest) and translates the
// resulting OpenAI response back into the caller's native shape.
//
// The goal is Pillar 4 of the moat plan: "accept any major SDK ever written,
// byte-exact replay guaranteed." An ingress adapter never bypasses the
// gateway's smart router, privacy layer, or recorder — it just parses and
// formats.
//
// Contract:
//
//   - ParseChatRequest(body []byte, pathParams) -> (*types.ChatCompletionRequest, error)
//   - FormatChatResponse(*types.ChatCompletionResponse) -> (body []byte, contentType string, error)
//   - FormatChatError(status, message) -> (body []byte, contentType string)
//
// The adapter signals which ingress it is via the returned request's
// `Ingress` field so recordings can replay byte-exact when asked.
package ingress

// Parser is implemented by each wire-format ingress package.
type Parser interface {
	Name() string
}
