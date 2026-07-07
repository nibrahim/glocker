package usage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// Sink persists captured samples.
//
// JSONLSink is the only implementation today; a database, an HTTP endpoint,
// or an in-memory sink (for tests) could implement the same interface.
type Sink interface {
	Write(Sample) error
	Close() error
}

// JSONLSink appends samples as JSON Lines (one JSON object per line) to a
// writer. Each Write is flushed so the log can be tailed live.
type JSONLSink struct {
	mu     sync.Mutex
	w      *bufio.Writer
	closer io.Closer
	enc    *json.Encoder
}

// NewJSONLSink writes samples to w. If w is also an io.Closer it will be
// closed by Close.
func NewJSONLSink(w io.Writer) *JSONLSink {
	bw := bufio.NewWriter(w)
	s := &JSONLSink{w: bw, enc: json.NewEncoder(bw)}
	if c, ok := w.(io.Closer); ok {
		s.closer = c
	}
	return s
}

// NewJSONLFileSink opens (creating parent dirs and appending) a JSONL log at
// path.
func NewJSONLFileSink(path string) (*JSONLSink, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create usage log dir: %w", err)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open usage log: %w", err)
	}
	return NewJSONLSink(f), nil
}

// Write encodes one sample as a JSON line and flushes it to the underlying
// writer.
func (s *JSONLSink) Write(sample Sample) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enc.Encode(sample); err != nil { // Encode appends a newline
		return fmt.Errorf("encode sample: %w", err)
	}
	return s.w.Flush()
}

// Close flushes buffered data and closes the underlying writer if it is a
// Closer.
func (s *JSONLSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.w.Flush(); err != nil {
		return err
	}
	if s.closer != nil {
		return s.closer.Close()
	}
	return nil
}
