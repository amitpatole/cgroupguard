package apply

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"bytes"
	"github.com/amitpatole/cgroupguard/internal/audit"
	"github.com/amitpatole/cgroupguard/internal/plan"
	appsv1 "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// Integration test against a REAL cluster (a disposable multi-node kind cluster).
// Set CGROUPGUARD_IT_CONTEXT to a kubeconfig context to run it. It creates and deletes a
// namespace, and cordons/drains/uncordons one worker node.
func TestIntegrationRealCluster(t *testing.T) {
	ctxName := os.Getenv("CGROUPGUARD_IT_CONTEXT")
	if ctxName == "" {
		t.Skip("set CGROUPGUARD_IT_CONTEXT to a disposable test cluster context to run")
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{CurrentContext: ctxName}).ClientConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := kubernetes.NewForConfigOrDie(cfg)
	ctx := context.Background()
	ns := "cgroupguard-it"
	_ = c.CoreV1().Namespaces().Delete(ctx, ns, meta.DeleteOptions{})
	time.Sleep(3 * time.Second)
	if _, err := c.CoreV1().Namespaces().Create(ctx, &core.Namespace{ObjectMeta: meta.ObjectMeta{Name: ns}}, meta.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CoreV1().Namespaces().Delete(ctx, ns, meta.DeleteOptions{}) })

	replicas := int32(3)
	lbl := map[string]string{"app": "demo"}
	skew := core.TopologySpreadConstraint{MaxSkew: 1, TopologyKey: "kubernetes.io/hostname", WhenUnsatisfiable: core.ScheduleAnyway, LabelSelector: &meta.LabelSelector{MatchLabels: lbl}}
	if _, err := c.AppsV1().Deployments(ns).Create(ctx, &appsv1.Deployment{ObjectMeta: meta.ObjectMeta{Name: "demo"}, Spec: appsv1.DeploymentSpec{Replicas: &replicas, Selector: &meta.LabelSelector{MatchLabels: lbl},
		Template: core.PodTemplateSpec{ObjectMeta: meta.ObjectMeta{Labels: lbl}, Spec: core.PodSpec{TopologySpreadConstraints: []core.TopologySpreadConstraint{skew}, TerminationGracePeriodSeconds: new(int64),
			Containers: []core.Container{{Name: "pause", Image: "registry.k8s.io/pause:3.10", ImagePullPolicy: core.PullIfNotPresent}}}}}}, meta.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	min := intstr.FromInt32(2)
	if _, err := c.PolicyV1().PodDisruptionBudgets(ns).Create(ctx, &policy.PodDisruptionBudget{ObjectMeta: meta.ObjectMeta{Name: "demo"}, Spec: policy.PodDisruptionBudgetSpec{MinAvailable: &min, Selector: &meta.LabelSelector{MatchLabels: lbl}}}, meta.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	ready := func() (int, map[string]int) {
		l, _ := c.CoreV1().Pods(ns).List(ctx, meta.ListOptions{LabelSelector: "app=demo"})
		n, byNode := 0, map[string]int{}
		for _, p := range l.Items {
			if p.DeletionTimestamp != nil || p.Status.Phase != core.PodRunning {
				continue
			}
			for _, cs := range p.Status.Conditions {
				if cs.Type == core.PodReady && cs.Status == core.ConditionTrue {
					n++
					byNode[p.Spec.NodeName]++
				}
			}
		}
		return n, byNode
	}
	for i := 0; ; i++ {
		if n, _ := ready(); n == 3 {
			break
		}
		if i > 90 {
			t.Fatal("demo pods did not become ready")
		}
		time.Sleep(2 * time.Second)
	}
	_, byNode := ready()
	target := ""
	for node, n := range byNode {
		if n > 0 && node != "" {
			target = node
		}
	}
	nodeObj, _ := c.CoreV1().Nodes().Get(ctx, target, meta.GetOptions{})
	if _, isCP := nodeObj.Labels["node-role.kubernetes.io/control-plane"]; isCP || target == "" {
		t.Fatalf("test needs a worker to drain, got %q", target)
	}
	t.Logf("draining %s which runs %d demo pod(s)", target, byNode[target])

	p := plan.Plan{SchemaVersion: plan.SchemaVersion, Target: "1.35", Groups: []plan.Group{{Provider: "eks", Pool: "old", Nodes: []string{target}}},
		Phases: []plan.Phase{{ID: "P3", Steps: []plan.Step{
			{ID: "M1.1", Title: "Cordon", Kind: "command", Disruption: plan.DisruptDrain, Approval: true, Rollback: []string{"uncordon"}, Verification: []string{"v"}, Action: &plan.Action{Type: "cordon", Nodes: []string{target}}},
			{ID: "M1.2", Title: "Drain", Kind: "command", Disruption: plan.DisruptDrain, Approval: true, Rollback: []string{"uncordon"}, Verification: []string{"v"}, Action: &plan.Action{Type: "drain", Nodes: []string{target}, TimeoutMinutes: 5}},
			{ID: "M1.3", Title: "Uncordon", Kind: "command", Disruption: plan.DisruptNone, Rollback: []string{"-"}, Verification: []string{"v"}, Action: &plan.Action{Type: "uncordon", Nodes: []string{target}}},
		}}}}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}

	// 1. Server-side dry run: nothing may change.
	var dryOut, dryLog bytes.Buffer
	if err := Run(ctx, c, p, Options{DryRun: true, Audit: audit.New(&dryLog, nil), Out: &dryOut}); err != nil {
		t.Fatalf("dry run: %v\n%s", err, dryOut.String())
	}
	if n, err := c.CoreV1().Nodes().Get(ctx, target, meta.GetOptions{}); err != nil || n.Spec.Unschedulable || n.Annotations[Marker] != "" {
		t.Fatalf("dry run changed the node: %+v", n.Spec)
	}
	if n, _ := ready(); n != 3 {
		t.Fatalf("dry run disturbed the pods: %d ready", n)
	}

	// 2. Real run while a watcher checks the budget is never violated.
	var mu sync.Mutex
	lowest := 3
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			if n, _ := ready(); true {
				mu.Lock()
				if n < lowest {
					lowest = n
				}
				mu.Unlock()
			}
			time.Sleep(200 * time.Millisecond)
		}
	}()
	var out, log bytes.Buffer
	conf := &scriptedConfirm{answers: []bool{true, true, true}}
	err = Run(ctx, c, p, Options{Confirm: conf, Audit: audit.New(&log, nil), Out: &out})
	close(stop)
	if err != nil {
		t.Fatalf("real run: %v\n%s", err, out.String())
	}
	mu.Lock()
	t.Logf("lowest ready count during the drain: %d (budget minAvailable=2)", lowest)
	if lowest < 2 {
		t.Fatalf("the PodDisruptionBudget was violated: only %d pods were ready at one point", lowest)
	}
	mu.Unlock()
	n, err := c.CoreV1().Nodes().Get(ctx, target, meta.GetOptions{})
	if err != nil || n.Spec.Unschedulable || n.Annotations[Marker] != "" {
		t.Fatalf("node not released cleanly: %+v %v", n.Spec, n.Annotations)
	}
	for i := 0; ; i++ {
		if r, by := ready(); r == 3 {
			t.Logf("final placement: %v", by)
			break
		}
		if i > 60 {
			t.Fatal("pods did not recover")
		}
		time.Sleep(2 * time.Second)
	}
	if v, err := audit.Verify(&log); err != nil || v != 7 {
		t.Fatalf("audit: %d %v", v, err)
	}
	t.Logf("dry run output:\n%s\nreal run output:\n%s", dryOut.String(), out.String())
}
