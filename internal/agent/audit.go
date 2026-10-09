package agent

import (
	"io"
	"time"

	"github.com/amitpatole/cgroupguard/internal/audit"
)

// Audit is the shared hash-chained log.
type Audit = audit.Log

type Entry = audit.Entry

func NewAudit(w io.Writer, now func() time.Time) *Audit { return audit.New(w, now) }

func VerifyLog(r io.Reader) (int, error) { return audit.Verify(r) }
