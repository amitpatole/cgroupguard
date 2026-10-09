package detect

import (
	"strings"
	"testing"
	"time"

	"github.com/amitpatole/cgroupguard/internal/model"
)

func FuzzParseCgroupVersionMetric(f *testing.F) {
	for _, s := range []string{"kubelet_cgroup_version 2\n", `kubelet_cgroup_version{a="b"} 1`, "", "kubelet_cgroup_version NaN", "kubelet_cgroup_version 1\nkubelet_cgroup_version 2", "kubelet_cgroup_versionx 2"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		v, e := ParseCgroupVersionMetric(strings.NewReader(in))
		if e == nil && v != 1 && v != 2 {
			t.Fatalf("accepted version %d from %q", v, in)
		}
	})
}

func FuzzPrometheusBody(f *testing.F) {
	f.Add(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"node":"a"},"value":[1800000000,"2"]}]}}`)
	f.Add(`{}`)
	f.Add(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,2]}]}}`)
	f.Fuzz(func(t *testing.T, in string) {
		nodes := []model.Node{{Name: "a", OS: "linux", Cgroup: "unknown"}, {Name: "b", OS: "linux", Cgroup: "unknown"}}
		_, _ = applyPrometheus(strings.NewReader(in), nodes, time.Unix(1_800_000_000, 0))
		for _, n := range nodes {
			if n.Cgroup != "unknown" && n.Cgroup != "v1" && n.Cgroup != "v2" {
				t.Fatalf("invalid mode %q", n.Cgroup)
			}
		}
	})
}

func FuzzPathSignals(f *testing.F) {
	f.Add("cat /sys/fs/cgroup/memory/memory.limit_in_bytes")
	f.Add("/sys/fs/cgroup/cpu,cpuacct/cpu.stat")
	f.Fuzz(func(t *testing.T, in string) {
		for _, p := range PathSignals([]string{in}) {
			ok := false
			for _, a := range CgroupPaths {
				ok = ok || p == a
			}
			if !ok {
				t.Fatalf("non-allowlisted signal %q", p)
			}
		}
	})
}
