package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/amitpatole/cgroupguard/internal/detect"
	"github.com/amitpatole/cgroupguard/internal/model"
	"github.com/amitpatole/cgroupguard/internal/report"
	"github.com/amitpatole/cgroupguard/internal/version"
	"github.com/spf13/cobra"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

// NewRoot builds the command tree. Run executes it; the documentation generator walks it.
func NewRoot(out, errOut io.Writer, code *int) *cobra.Command {
	root := &cobra.Command{Use: "cgroupguard", Short: "Evidence-based cgroup v1 to v2 readiness checker", Version: version.Version, SilenceErrors: true, SilenceUsage: true}
	root.SetOut(out)
	root.SetErr(errOut)
	var format, output, failOn string
	var exitError bool
	var so scanOpts
	check := &cobra.Command{Use: "check", Short: "Read node inventory; probe nodes only with --probe", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("output") {
			if cmd.Flags().Changed("format") && output != format {
				return fmt.Errorf("--output and --format disagree; use one")
			}
			format = output
		}
		switch format {
		case "table", "json", "markdown", "sarif", "kubent-compatible":
		default:
			return fmt.Errorf("format must be table, json, markdown, sarif or kubent-compatible")
		}
		threshold, ok := failRank[failOn]
		if !ok {
			return fmt.Errorf("--fail-on must be high, warning, note or none")
		}
		r, e := so.scan(cmd)
		if e != nil {
			return e
		}
		if e = report.Write(out, r, format); e != nil {
			return e
		}
		*code = exitCode(r, threshold, exitError)
		if exitError && *code == 0 {
			_, _ = fmt.Fprintln(errOut, "note: no findings at or above --fail-on, but coverage is incomplete; exit 0 does not certify upgrade readiness")
		}
		return nil
	}}
	f := check.Flags()
	so.bind(f)
	f.StringVar(&format, "format", "table", "table, json, markdown, sarif or kubent-compatible")
	f.StringVarP(&output, "output", "o", "table", "Alias of --format (kubent-style)")
	f.StringVar(&failOn, "fail-on", "warning", "Lowest severity that counts as a failing finding: high, warning, note or none")
	f.BoolVarP(&exitError, "exit-error", "e", false, "kubent-style gating: exit 200 on findings at or above --fail-on, else 0 (default mode exits 1 while coverage is incomplete)")
	check.MarkFlagsMutuallyExclusive("input", "local")
	var probePath string
	probeCmd := &cobra.Command{Use: "probe", Short: "Print local filesystem evidence as JSON (used inside probe pods)", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		mode, evidence, e := detect.Local(probePath)
		if e != nil {
			return e
		}
		return json.NewEncoder(out).Encode(struct {
			Cgroup   string `json:"cgroup"`
			Evidence string `json:"evidence"`
		}{mode, evidence})
	}}
	probeCmd.Flags().StringVar(&probePath, "path", "/sys/fs/cgroup", "Cgroup root to inspect")
	root.AddCommand(check, fixCommand(out, errOut), agentCommand(out, errOut, code), controllerCommand(), probeCmd)
	return root
}

func Run(args []string, out, errOut io.Writer) int {
	code := 0
	root := NewRoot(out, errOut, &code)
	root.SetArgs(args)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if e := root.ExecuteContext(ctx); e != nil {
		_, _ = fmt.Fprintf(errOut, "Error: %s\n", e)
		return 2
	}
	return code
}

var failRank = map[string]int{"high": 0, "warning": 1, "note": 2, "none": -1}
var severityRank = map[string]int{"high": 0, "warning": 1, "note": 2}

// exitCode implements the documented contract. Default: 0 only when nothing fails
// and coverage is complete (never true yet), otherwise 1. With --exit-error
// (kubent semantics): 200 on findings at or above the threshold, else 0.
func exitCode(r model.Report, threshold int, exitError bool) int {
	hit := false
	if threshold >= 0 {
		for _, f := range r.Findings {
			if severityRank[f.Severity] <= threshold {
				hit = true
				break
			}
		}
	}
	switch {
	case exitError && hit:
		return 200
	case exitError:
		return 0
	case hit || r.ClusterReadiness != "ready":
		return 1
	}
	return 0
}

const maxSnapshot = 16 * 1024 * 1024

// openSnapshotLike opens any user-named regular file with the same confinement rules.
func openSnapshotLike(path string) (*os.File, error) { return openSnapshot(path) }

// openSnapshot opens a regular file only. Devices, FIFOs and directories are
// rejected before any read so a hostile path cannot block or exhaust the process.
func openSnapshot(path string) (*os.File, error) {
	clean := filepath.Clean(path)
	root, e := os.OpenRoot(filepath.Dir(clean))
	if e != nil {
		return nil, fmt.Errorf("open %s: %w", path, e)
	}
	defer root.Close()
	f, e := root.Open(filepath.Base(clean))
	if e != nil {
		return nil, fmt.Errorf("open %s: %w", path, e)
	}
	fi, e := f.Stat()
	if e == nil && !fi.Mode().IsRegular() {
		e = fmt.Errorf("%s is not a regular file", path)
	}
	if e == nil && fi.Size() > maxSnapshot {
		e = fmt.Errorf("snapshot exceeds 16 MiB")
	}
	if e != nil {
		return nil, errors.Join(e, f.Close())
	}
	return f, nil
}

func ReadSnapshot(r io.Reader) (model.Snapshot, error) {
	var s model.Snapshot
	data, e := io.ReadAll(io.LimitReader(r, 16*1024*1024+1))
	if e != nil {
		return s, e
	}
	if len(data) > 16*1024*1024 {
		return s, fmt.Errorf("snapshot exceeds 16 MiB")
	}
	// Decoder rejects typos and trailing documents instead of silently ignoring them.
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if e = d.Decode(&s); e != nil {
		return s, e
	}
	var extra any
	if e = d.Decode(&extra); e != io.EOF {
		return s, fmt.Errorf("snapshot must contain exactly one JSON document")
	}
	if s.SchemaVersion != 1 {
		return s, fmt.Errorf("unsupported snapshot schemaVersion %d", s.SchemaVersion)
	}
	return s, nil
}
