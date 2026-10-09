// Package plan builds a deterministic, reviewable remediation plan from a scan
// report. Nothing here executes anything: commands are text for a human to review.
package plan

import (
	"fmt"
	"regexp"
	"strings"
)

const SchemaVersion = 1

// Disruption levels, from none to most disruptive.
const (
	DisruptNone     = "none"
	DisruptRolling  = "rolling"       // pods restart in a controlled rollout
	DisruptDrain    = "node-drain"    // pods are evicted from a node
	DisruptReboot   = "node-reboot"   // a node restarts
	DisruptReplace  = "node-replace"  // nodes are replaced by new ones
	DisruptConfig   = "config-change" // kubelet or runtime configuration changes
	DisruptPlatform = "platform"      // changes managed by a cloud provider API
)

// Step is one reviewable action. Steps that disrupt anything must carry Rollback
// and Verification (enforced by Validate), and require human approval.
type Step struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Kind          string   `json:"kind"` // inspect | command | iac | config | manual
	Disruption    string   `json:"disruption"`
	Approval      bool     `json:"requiresApproval"`
	Scope         string   `json:"scope,omitempty"`
	Details       string   `json:"details,omitempty"`
	Preconditions []string `json:"preconditions,omitempty"`
	Commands      []string `json:"commands,omitempty"`
	Verification  []string `json:"verification,omitempty"`
	Rollback      []string `json:"rollback,omitempty"`
	Sources       []string `json:"sources,omitempty"`
	Files         []File   `json:"files,omitempty"`  // content for a human to save and review
	Action        *Action  `json:"action,omitempty"` // set only when apply can perform the step natively
}

// Action is a typed operation cgroupguard can perform itself, with its own safety checks,
// when the operator chooses --apply. Plan text is never executed; only Actions are.
type Action struct {
	Type           string   `json:"type"` // cordon | drain | uncordon
	Nodes          []string `json:"nodes"`
	TimeoutMinutes int      `json:"timeoutMinutes,omitempty"` // drain only
}

// File is review material attached to a step, written only to --output-dir.
type File struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type Phase struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Steps []Step `json:"steps"`
}

// Group is a set of nodes migrated together: one managed node pool, or the
// self-managed nodes of one provider.
type Group struct {
	Provider string   `json:"provider"`
	Pool     string   `json:"pool,omitempty"`
	Nodes    []string `json:"nodes"`
	Pods     *int     `json:"podsAffected,omitempty"`
	Canary   bool     `json:"canary,omitempty"`
	Arch     string   `json:"arch,omitempty"`         // set only when all nodes agree
	Instance string   `json:"instanceType,omitempty"` // set only when all nodes agree and the value is a safe name
	OSFamily string   `json:"osFamily,omitempty"`     // ubuntu | azurelinux | other (from the node OS image)
	Stateful bool     `json:"stateful,omitempty"`     // a workload with persistent storage runs on these nodes
}

type Plan struct {
	SchemaVersion int      `json:"schemaVersion"`
	Target        string   `json:"target"`
	Groups        []Group  `json:"groups"`
	Phases        []Phase  `json:"phases"`
	Assumptions   []string `json:"assumptions"`
	Limitations   []string `json:"limitations"`
}

var (
	idPattern   = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.-]*$`)
	namePattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]{0,251}[A-Za-z0-9])?$`)
)

// SafeName reports whether a cluster-supplied name may be placed into commands and
// generated files. Names outside this set are never interpolated.
func SafeName(s string) bool { return namePattern.MatchString(s) && !strings.Contains(s, "..") }

// Display renders a cluster- or file-supplied string for prose. Only characters that
// are inert in Markdown, terminals and shells survive; everything else becomes '?'.
func Display(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n >= 120 {
			b.WriteString("...")
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_', r == '/', r == ':':
			b.WriteRune(r)
		default:
			b.WriteByte('?')
		}
		n++
	}
	return b.String()
}

func displayAll(l []string) []string {
	out := make([]string, len(l))
	for i, s := range l {
		out[i] = Display(s)
	}
	return out
}

// Quote single-quotes a value for POSIX shells. Validated names pass through
// unchanged; anything else is escaped, never trusted.
func Quote(s string) string {
	if SafeName(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Validate enforces the plan invariants: unique IDs, rollback and verification on
// every disruptive step, approval on every disruptive step, and sources on any
// step that makes a platform-specific claim.
func (p Plan) Validate() error {
	if p.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported plan schema %d", p.SchemaVersion)
	}
	seen := map[string]bool{}
	for _, ph := range p.Phases {
		if !idPattern.MatchString(ph.ID) {
			return fmt.Errorf("invalid phase id %q", ph.ID)
		}
		for _, s := range ph.Steps {
			if !idPattern.MatchString(s.ID) || seen[s.ID] {
				return fmt.Errorf("invalid or duplicate step id %q", s.ID)
			}
			seen[s.ID] = true
			if s.Title == "" {
				return fmt.Errorf("step %s has no title", s.ID)
			}
			switch s.Kind {
			case "inspect", "command", "iac", "config", "manual":
			default:
				return fmt.Errorf("step %s has invalid kind %q", s.ID, s.Kind)
			}
			if s.Disruption != DisruptNone {
				if len(s.Rollback) == 0 {
					return fmt.Errorf("disruptive step %s has no rollback", s.ID)
				}
				if len(s.Verification) == 0 {
					return fmt.Errorf("disruptive step %s has no verification", s.ID)
				}
				if !s.Approval {
					return fmt.Errorf("disruptive step %s does not require approval", s.ID)
				}
			}
			if s.Disruption == "" {
				return fmt.Errorf("step %s has no disruption level", s.ID)
			}
			if a := s.Action; a != nil {
				switch a.Type {
				case "cordon", "drain", "uncordon":
				default:
					return fmt.Errorf("step %s has unknown action %q", s.ID, a.Type)
				}
				if len(a.Nodes) == 0 || len(a.Nodes) > 500 {
					return fmt.Errorf("step %s action has an invalid node list", s.ID)
				}
				for _, n := range a.Nodes {
					if !SafeName(n) {
						return fmt.Errorf("step %s action names an unsafe node", s.ID)
					}
				}
				if a.Type == "drain" && (a.TimeoutMinutes < 1 || a.TimeoutMinutes > 120) {
					return fmt.Errorf("step %s drain timeout out of range", s.ID)
				}
				if s.Kind != "command" || a.Type != "uncordon" && !s.Approval {
					return fmt.Errorf("step %s action must be a command step that requires approval", s.ID)
				}
			}
		}
	}
	return nil
}
