package report_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/amitpatole/cgroupguard/internal/model"
	"github.com/amitpatole/cgroupguard/internal/report"
	"github.com/amitpatole/cgroupguard/internal/rules"
)

func TestReportGolden(t *testing.T) {
	data, e := os.ReadFile("../../examples/workloads.json")
	if e != nil {
		t.Fatal(e)
	}
	var s model.Snapshot
	if e = json.Unmarshal(data, &s); e != nil {
		t.Fatal(e)
	}
	set, e := rules.Load()
	if e != nil {
		t.Fatal(e)
	}
	r, e := rules.Evaluate(set, s.Nodes)
	if e != nil {
		t.Fatal(e)
	}
	if e = rules.AddWorkloads(&r, s.Workloads); e != nil {
		t.Fatal(e)
	}
	for _, format := range []string{"table", "json", "markdown", "sarif", "kubent-compatible"} {
		t.Run(format, func(t *testing.T) {
			var b bytes.Buffer
			if e := report.Write(&b, r, format); e != nil {
				t.Fatal(e)
			}
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if e := os.WriteFile(filepath.Join("testdata", format+".golden"), b.Bytes(), 0o644); e != nil {
					t.Fatal(e)
				}
			}
			want, e := os.ReadFile(filepath.Join("testdata", format+".golden"))
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(b.Bytes(), want) {
				t.Fatal("report differs from reviewed golden output")
			}
		})
	}
}
