package privacy

import (
	"bytes"
	"strings"
)

// Rehydrator reverses a Redactor's substitutions given the same Mapping
// the Vault persisted. Supports two modes:
//
//   - Whole — for non-streaming JSON bodies.
//   - Streaming — stateful; buffers just enough bytes to hold a
//     half-emitted token across chunk boundaries.
//
// The implementation is deliberately simple: because all tokens are of
// the shape `<<pii:CATEGORY:HEX>>` (fixed small upper bound on length)
// the streaming path only ever needs to buffer that many bytes.
type Rehydrator struct {
	mapping Mapping
}

func NewRehydrator(m Mapping) *Rehydrator {
	return &Rehydrator{mapping: m}
}

// maxTokenLen caps the look-ahead the streaming rehydrator holds back.
// Pii tokens are <<pii:CATEGORY:HEX10>> which max out near 32 chars;
// 64 is a safe ceiling even with long custom categories.
const maxTokenLen = 64

// Whole replaces every occurrence of every token in mapping with its
// original value. Safe for JSON bodies because tokens are ASCII and
// never collide with JSON syntax.
func (r *Rehydrator) Whole(s string) string {
	if r == nil || len(r.mapping.Entries) == 0 || !strings.Contains(s, "<<pii:") {
		return s
	}
	out := s
	for tok, orig := range r.mapping.Entries {
		if !strings.Contains(out, tok) {
			continue
		}
		out = strings.ReplaceAll(out, tok, orig)
	}
	return out
}

// StreamWriter wraps an io.Writer and rewrites tokens in-flight. Call
// Flush at the end of the response to emit any trailing buffered data.
type StreamWriter struct {
	mapping Mapping
	buf     bytes.Buffer
	emit    func(p []byte) (int, error)
}

// NewStreamWriter returns a streaming rehydrator that calls emit for
// the rewritten output. emit is typically (w io.Writer).Write.
func NewStreamWriter(m Mapping, emit func(p []byte) (int, error)) *StreamWriter {
	return &StreamWriter{mapping: m, emit: emit}
}

// Write buffers incoming bytes and flushes whatever cannot possibly
// straddle a token boundary (i.e. everything except the last
// maxTokenLen bytes).
func (s *StreamWriter) Write(p []byte) (int, error) {
	n := len(p)
	s.buf.Write(p)
	if s.buf.Len() <= maxTokenLen {
		return n, nil
	}
	// Flush everything except a safety tail.
	head := s.buf.Len() - maxTokenLen
	if head <= 0 {
		return n, nil
	}
	chunk := s.buf.Bytes()[:head]
	rewritten := s.rewrite(chunk)
	if _, err := s.emit(rewritten); err != nil {
		return 0, err
	}
	s.buf.Next(head)
	return n, nil
}

// Flush rewrites and emits any buffered bytes. Call once at response end.
func (s *StreamWriter) Flush() error {
	if s.buf.Len() == 0 {
		return nil
	}
	rewritten := s.rewrite(s.buf.Bytes())
	s.buf.Reset()
	if _, err := s.emit(rewritten); err != nil {
		return err
	}
	return nil
}

func (s *StreamWriter) rewrite(p []byte) []byte {
	if len(s.mapping.Entries) == 0 || !bytes.Contains(p, []byte("<<pii:")) {
		return p
	}
	out := p
	for tok, orig := range s.mapping.Entries {
		out = bytes.ReplaceAll(out, []byte(tok), []byte(orig))
	}
	return out
}
