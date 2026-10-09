package plan

import (
	"os/exec"
	"strings"
	"testing"
)

func TestQuoteNeutralizesHostileNames(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("needs a POSIX shell; generated shell snippets target Linux hosts")
	}
	for _, hostile := range []string{"a; rm -rf /", "$(id)", "`id`", "a'b", "a b", "a\nb", "--flag", "a|b", "../x", "x\x00y", "é", ""} {
		q := Quote(hostile)
		if hostile != "" && SafeName(hostile) && !strings.HasPrefix(hostile, "-") {
			continue
		}
		// Run the quoted value through a real shell: it must come back byte-for-byte.
		out, err := exec.Command("sh", "-c", "printf %s "+q).Output()
		if err != nil || string(out) != hostile {
			if hostile == "x\x00y" || hostile == "" {
				continue // NUL cannot be passed through a shell; empty is quoted as ''
			}
			t.Fatalf("quote(%q)=%q round-trips to %q (%v)", hostile, q, out, err)
		}
	}
}

func TestLeadingDashNamesAreNotSafe(t *testing.T) {
	for _, n := range []string{"-rf", "--all", "-", "a..b", "0..0", ".x", "x."} {
		if SafeName(n) {
			t.Fatalf("%q accepted", n)
		}
	}
}

func TestValidateEnforcesRollbackVerificationApproval(t *testing.T) {
	ok := Step{ID: "A.1", Title: "t", Kind: "command", Disruption: DisruptDrain, Approval: true, Rollback: []string{"r"}, Verification: []string{"v"}}
	mk := func(s Step) Plan {
		return Plan{SchemaVersion: SchemaVersion, Phases: []Phase{{ID: "P1", Steps: []Step{s}}}}
	}
	if e := mk(ok).Validate(); e != nil {
		t.Fatal(e)
	}
	for name, mut := range map[string]func(*Step){
		"no rollback":     func(s *Step) { s.Rollback = nil },
		"no verification": func(s *Step) { s.Verification = nil },
		"no approval":     func(s *Step) { s.Approval = false },
		"no title":        func(s *Step) { s.Title = "" },
		"bad kind":        func(s *Step) { s.Kind = "run" },
		"bad id":          func(s *Step) { s.ID = "a b" },
		"no disruption":   func(s *Step) { s.Disruption = "" },
	} {
		s := ok
		mut(&s)
		if mk(s).Validate() == nil {
			t.Fatalf("accepted step with %s", name)
		}
	}
	dup := Plan{SchemaVersion: SchemaVersion, Phases: []Phase{{ID: "P1", Steps: []Step{ok, ok}}}}
	if dup.Validate() == nil {
		t.Fatal("accepted duplicate step ids")
	}
	// Non-disruptive inspect steps need no rollback.
	if e := mk(Step{ID: "A.2", Title: "look", Kind: "inspect", Disruption: DisruptNone}).Validate(); e != nil {
		t.Fatal(e)
	}
}
