// Package session persists agent conversations as JSONL transcripts so they
// can be resumed in a later run.
package session

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/russellhaering/autoswe/pkg/llm"
)

// Session is a persisted conversation: a stable ID and the full message
// history that produced it.
type Session struct {
	ID       string
	Messages []llm.Message
}

// NewID generates a short random session id like "sess_a1b2c3d4".
func NewID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return "sess_" + hex.EncodeToString(b[:])
}

// DefaultDir returns ~/.autoswe/sessions, creating it on demand.
func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".autoswe", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// PathFor returns the on-disk transcript path for id under dir.
func PathFor(dir, id string) string {
	return filepath.Join(dir, id+".jsonl")
}

// Save writes messages to the session's transcript, overwriting any prior
// file. Each line is one llm.Message JSON object.
func Save(dir, id string, messages []llm.Message) error {
	if id == "" {
		return errors.New("session: id is required")
	}
	path := PathFor(dir, id)
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("session save: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, m := range messages {
		if err := enc.Encode(m); err != nil {
			return fmt.Errorf("session save encode: %w", err)
		}
	}
	return nil
}

// Load reads the transcript at <dir>/<id>.jsonl. Empty/missing files yield an
// error.
func Load(dir, id string) (Session, error) {
	path := PathFor(dir, id)
	f, err := os.Open(path)
	if err != nil {
		return Session{}, fmt.Errorf("session load: %w", err)
	}
	defer f.Close()

	s := Session{ID: id}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var m llm.Message
		if err := json.Unmarshal(scanner.Bytes(), &m); err != nil {
			return Session{}, fmt.Errorf("session load decode: %w", err)
		}
		s.Messages = append(s.Messages, m)
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return Session{}, fmt.Errorf("session load scan: %w", err)
	}
	if len(s.Messages) == 0 {
		return Session{}, fmt.Errorf("session %s is empty", id)
	}
	return s, nil
}
