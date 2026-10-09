package detect

import (
	"context"
	"github.com/amitpatole/cgroupguard/internal/model"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"strings"
	"testing"
)

func TestFailedProbeDeletesExactUID(t *testing.T) {
	c := fake.NewClientset()
	p := &core.Pod{ObjectMeta: meta.ObjectMeta{Name: "probe-created", Namespace: "scan", UID: "unique-probe-id"}, Status: core.PodStatus{Phase: core.PodFailed}}
	c.PrependReactor("create", "pods", func(a ktesting.Action) (bool, runtime.Object, error) { return true, p, nil })
	c.PrependReactor("get", "pods", func(a ktesting.Action) (bool, runtime.Object, error) { return true, p, nil })
	deleted := false
	c.PrependReactor("delete", "pods", func(a ktesting.Action) (bool, runtime.Object, error) {
		d := a.(ktesting.DeleteAction)
		if d.GetName() != p.Name || d.GetDeleteOptions().Preconditions == nil || *d.GetDeleteOptions().Preconditions.UID != p.UID {
			t.Fatal("cleanup did not constrain exact pod UID")
		}
		deleted = true
		return true, nil, nil
	})
	n := model.Node{Name: "n", OS: "linux", Cgroup: "unknown"}
	err := Probe(context.Background(), c, &n, "scan", "registry/probe@sha256:"+strings.Repeat("a", 64))
	if err == nil || !deleted || n.Cgroup != "unknown" {
		t.Fatalf("err=%v deleted=%v node=%+v", err, deleted, n)
	}
}

func TestInvalidDigestNeverCreatesPod(t *testing.T) {
	c := fake.NewClientset()
	n := model.Node{Name: "n", OS: "linux"}
	for _, image := range []string{"image:latest", "image@sha256:abc"} {
		if Probe(context.Background(), c, &n, "scan", image) == nil {
			t.Fatal("accepted invalid digest")
		}
	}
	if len(c.Actions()) != 0 {
		t.Fatal("invalid image caused API action")
	}
}

func TestHybridAndUnknown(t *testing.T) {
	for _, tc := range []struct {
		a, b bool
		want string
	}{{false, false, "unknown"}, {true, false, "v1"}, {true, true, "hybrid"}, {false, true, "unknown"}} {
		if got := classify(tc.a, tc.b); got != tc.want {
			t.Fatalf("%+v: %s", tc, got)
		}
	}
}

func TestInventoryNeverWritesOrInfersV2(t *testing.T) {
	c := fake.NewClientset(&core.Node{ObjectMeta: meta.ObjectMeta{Name: "n", Labels: map[string]string{"eks.amazonaws.com/nodegroup": "pool"}}, Status: core.NodeStatus{NodeInfo: core.NodeSystemInfo{OperatingSystem: "linux", OSImage: "Amazon Linux 2023", KubeletVersion: "v1.35.0"}}})
	n, e := Inventory(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	if len(n) != 1 || n[0].Cgroup != "unknown" || n[0].Provider != "eks" {
		t.Fatalf("%+v", n)
	}
	for _, a := range c.Actions() {
		if a.GetVerb() != "list" {
			t.Fatalf("unexpected action %v", a)
		}
	}
}

func TestProbePodSecurity(t *testing.T) {
	p := ProbePod("node", "scan", "registry/image@sha256:abc")
	s := p.Spec
	c := s.Containers[0]
	if s.HostPID || s.HostNetwork || s.HostIPC || *s.AutomountServiceAccountToken {
		t.Fatal("unsafe host access")
	}
	if !*s.SecurityContext.RunAsNonRoot || *c.SecurityContext.AllowPrivilegeEscalation || !*c.SecurityContext.ReadOnlyRootFilesystem {
		t.Fatal("unsafe security context")
	}
	if len(s.Volumes) != 1 || s.Volumes[0].HostPath.Path != "/sys/fs/cgroup" || !c.VolumeMounts[0].ReadOnly {
		t.Fatal("unsafe mount")
	}
	if s.NodeName != "node" || s.ActiveDeadlineSeconds == nil {
		t.Fatal("missing placement/deadline")
	}
}
