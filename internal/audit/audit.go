// Package audit is an append-only, hash-chained JSON Lines log shared by every feature that
// can act or call out (the assistant, apply, pull-request creation).
package audit

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Log is an append-only JSON Lines log. Each entry commits to the previous one
// (SHA-256 chain), so edits, deletions and reordering are detectable by Verify.
// Entries hold hashes, sizes, tool names and redacted arguments - never credentials
// and never unredacted data.
type Log struct {
	mu   sync.Mutex
	w    io.Writer
	now  func() time.Time
	seq  int
	prev string
}

type Entry struct {
	Seq    int            `json:"seq"`
	Time   string         `json:"time"`
	Prev   string         `json:"prev"`
	Kind   string         `json:"kind"`
	Detail map[string]any `json:"detail,omitempty"`
	Hash   string         `json:"hash"`
}

const genesis = "0000000000000000000000000000000000000000000000000000000000000000"

func New(w io.Writer, now func() time.Time) *Log {
	if now == nil {
		now = time.Now
	}
	return &Log{w: w, now: now, prev: genesis}
}

func entryHash(e Entry) string {
	d, _ := json.Marshal(e.Detail) // map keys are sorted, so this is canonical
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s|%s|%s", e.Seq, e.Time, e.Prev, e.Kind, d)))
	return hex.EncodeToString(sum[:])
}

// Log appends one entry. A nil Log discards (used when the user asked for no log).
func (a *Log) Log(kind string, detail map[string]any) error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.seq++
	e := Entry{Seq: a.seq, Time: a.now().UTC().Format(time.RFC3339Nano), Prev: a.prev, Kind: kind, Detail: detail}
	e.Hash = entryHash(e)
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err := a.w.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("audit log write failed: %w", err)
	}
	a.prev = e.Hash
	return nil
}

// Verify checks the hash chain and returns the number of entries.
func Verify(r io.Reader) (int, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	prev, n := genesis, 0
	for sc.Scan() {
		var e Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			return n, fmt.Errorf("entry %d is not valid JSON", n+1)
		}
		n++
		if e.Seq != n {
			return n, fmt.Errorf("entry %d has sequence number %d (entries were removed, added or reordered)", n, e.Seq)
		}
		if e.Prev != prev {
			return n, fmt.Errorf("entry %d does not follow entry %d (chain broken)", n, n-1)
		}
		if entryHash(e) != e.Hash {
			return n, fmt.Errorf("entry %d was modified (hash mismatch)", n)
		}
		prev = e.Hash
	}
	return n, sc.Err()
}

// Create starts a new log file at path. The file must not exist (so the hash chain starts
// clean), is created with mode 0600, and is opened through a directory-confined root so a
// symlink in the path cannot redirect the write.
func Create(path string) (*Log, func(), error) {
	clean := filepath.Clean(path)
	root, err := os.OpenRoot(filepath.Dir(clean))
	if err != nil {
		return nil, nil, fmt.Errorf("audit log: %w", err)
	}
	defer root.Close()
	f, err := root.OpenFile(filepath.Base(clean), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("audit log: %w (the file must not exist so the hash chain starts clean)", err)
	}
	return New(f, nil), func() { _ = f.Close() }, nil
}
