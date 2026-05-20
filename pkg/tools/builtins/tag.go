package builtins

import (
	"crypto/sha256"
	"encoding/base64"
)

// lineTag returns a 4-char base64-url tag for a line's raw bytes,
// excluding any trailing newline. Same format as antirez's content
// hashes in https://antirez.com/news/166. Used by `read` to stamp
// every emitted line and by `patch` as a CAS check.
//
// 24 bits / ~16M space → per-edit false-accept probability ~6e-8.
func lineTag(line []byte) string {
	sum := sha256.Sum256(line)
	return base64.RawURLEncoding.EncodeToString(sum[:3])
}
