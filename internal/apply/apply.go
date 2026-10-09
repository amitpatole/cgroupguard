// Package apply performs a small set of typed Kubernetes actions from a plan, one
// step at a time, each behind a typed human confirmation. It never runs plan text,
// never forces, and never bypasses PodDisruptionBudgets.
package apply

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/amitpatole/cgroupguard/internal/audit"
	"github.com/amitpatole/cgroupguard/internal/plan"
	core "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// Marker annotation shared with the generated Ansible role: nodes carrying it were
// cordoned by cgroupguard, and only those are uncordoned automatically.
const Marker = "cgroupguard.io/migrating"

// Confirmer asks a human to approve one step. It must return true only for an
// explicit, typed approval.
type Confirmer interface {
	Confirm(step plan.Step) (bool, error)
}

type Options struct {
	DryRun  bool
	Confirm Confirmer
	Audit   *audit.Log
	Out     io.Writer
	// Sleep and Poll are injectable for tests.
	Sleep func(time.Duration)
	Poll  time.Duration
}

var ErrDeclined = errors.New("step declined; stopping")

func (o *Options) sleep(d time.Duration) {
	if o.Sleep != nil {
		o.Sleep(d)
		return
	}
	time.Sleep(d)
}

func (o *Options) say(format string, a ...any) {
	if o.Out != nil {
		_, _ = fmt.Fprintf(o.Out, format+"\n", a...)
	}
}

// Run executes every step that has an Action, in plan order, stopping at the first
// failure or declined step. Steps without an Action are listed as manual.
func Run(ctx context.Context, c kubernetes.Interface, p plan.Plan, o Options) error {
	if err := p.Validate(); err != nil {
		return fmt.Errorf("refusing to apply an invalid plan: %w", err)
	}
	if !o.DryRun && o.Confirm == nil {
		return fmt.Errorf("a confirmer is required for a real run")
	}
	known := map[string]bool{}
	for _, g := range p.Groups {
		for _, n := range g.Nodes {
			known[n] = true
		}
	}
	mode := "APPLY"
	if o.DryRun {
		mode = "DRY RUN (server-side; nothing is changed)"
	}
	o.say("== %s ==", mode)
	_ = o.Audit.Log("apply_start", map[string]any{"dryRun": o.DryRun, "target": p.Target})
	done := 0
	for _, ph := range p.Phases {
		for _, s := range ph.Steps {
			if s.Action == nil {
				if s.Disruption != plan.DisruptNone {
					o.say("[manual] %s %s (not performed by apply; see RUNBOOK.md)", s.ID, s.Title)
				}
				continue
			}
			for _, n := range s.Action.Nodes {
				if !known[n] {
					return fmt.Errorf("step %s names node %q that is not part of the migration groups", s.ID, n)
				}
			}
			o.say("\nStep %s: %s", s.ID, s.Title)
			o.say("  action: %s %s", s.Action.Type, strings.Join(s.Action.Nodes, " "))
			for _, r := range s.Rollback {
				o.say("  rollback: %s", r)
			}
			if !o.DryRun {
				ok, err := o.Confirm.Confirm(s)
				if err != nil {
					return err
				}
				if !ok {
					_ = o.Audit.Log("declined", map[string]any{"step": s.ID})
					return ErrDeclined
				}
				_ = o.Audit.Log("confirmed", map[string]any{"step": s.ID, "action": s.Action.Type, "nodes": s.Action.Nodes})
			}
			if err := perform(ctx, c, s, &o); err != nil {
				_ = o.Audit.Log("failed", map[string]any{"step": s.ID, "error": err.Error()})
				o.say("  FAILED: %v", err)
				for _, r := range s.Rollback {
					o.say("  rollback: %s", r)
				}
				return fmt.Errorf("step %s failed: %w", s.ID, err)
			}
			_ = o.Audit.Log("done", map[string]any{"step": s.ID, "dryRun": o.DryRun})
			done++
		}
	}
	o.say("\n%d step(s) %s.", done, map[bool]string{true: "checked", false: "performed"}[o.DryRun])
	return nil
}

func perform(ctx context.Context, c kubernetes.Interface, s plan.Step, o *Options) error {
	a := s.Action
	for _, n := range a.Nodes {
		switch a.Type {
		case "cordon":
			if err := setCordon(ctx, c, n, true, o); err != nil {
				return err
			}
		case "uncordon":
			if err := setCordon(ctx, c, n, false, o); err != nil {
				return err
			}
		case "drain":
			if err := drain(ctx, c, n, time.Duration(a.TimeoutMinutes)*time.Minute, o); err != nil {
				return err
			}
		}
	}
	return nil
}

func dry(o *Options) []string {
	if o.DryRun {
		return []string{meta.DryRunAll}
	}
	return nil
}

func setCordon(ctx context.Context, c kubernetes.Interface, name string, on bool, o *Options) error {
	node, err := c.CoreV1().Nodes().Get(ctx, name, meta.GetOptions{})
	if err != nil {
		return fmt.Errorf("node %s: %w", name, err)
	}
	if on {
		if node.Spec.Unschedulable {
			if node.Annotations[Marker] != "true" {
				o.say("  node %s was already cordoned by someone else; leaving it as is and not marking it", name)
				return nil
			}
			o.say("  node %s is already cordoned by cgroupguard", name)
			return nil
		}
		patch := `{"spec":{"unschedulable":true},"metadata":{"annotations":{"` + Marker + `":"true"}}}`
		if _, err := c.CoreV1().Nodes().Patch(ctx, name, types.StrategicMergePatchType, []byte(patch), meta.PatchOptions{DryRun: dry(o)}); err != nil {
			return fmt.Errorf("cordon %s: %w", name, err)
		}
		if !o.DryRun {
			if after, err := c.CoreV1().Nodes().Get(ctx, name, meta.GetOptions{}); err != nil || !after.Spec.Unschedulable {
				return fmt.Errorf("cordon %s could not be verified", name)
			}
		}
		o.say("  cordoned %s", name)
		return nil
	}
	if !node.Spec.Unschedulable {
		o.say("  node %s is already schedulable", name)
		return nil
	}
	if node.Annotations[Marker] != "true" {
		return fmt.Errorf("node %s is cordoned but was not cordoned by cgroupguard (no %s annotation); refusing to release it", name, Marker)
	}
	patch := `{"spec":{"unschedulable":false},"metadata":{"annotations":{"` + Marker + `":null}}}`
	if _, err := c.CoreV1().Nodes().Patch(ctx, name, types.StrategicMergePatchType, []byte(patch), meta.PatchOptions{DryRun: dry(o)}); err != nil {
		return fmt.Errorf("uncordon %s: %w", name, err)
	}
	o.say("  uncordoned %s", name)
	return nil
}

// evictable splits the pods on a node. Pods that would be lost (no controller) make
// the drain refuse, exactly like kubectl drain without --force.
func evictable(ctx context.Context, c kubernetes.Interface, node string) (pods []core.Pod, problems []string, err error) {
	var token string
	for {
		list, e := c.CoreV1().Pods("").List(ctx, meta.ListOptions{FieldSelector: "spec.nodeName=" + node, Limit: 500, Continue: token})
		if e != nil {
			return nil, nil, e
		}
		for _, p := range list.Items {
			if p.Spec.NodeName != node {
				continue // field selectors are advisory for some servers and fakes
			}
			if _, mirror := p.Annotations[core.MirrorPodAnnotationKey]; mirror {
				continue
			}
			var ctrl *meta.OwnerReference
			for i := range p.OwnerReferences {
				if p.OwnerReferences[i].Controller != nil && *p.OwnerReferences[i].Controller {
					ctrl = &p.OwnerReferences[i]
				}
			}
			switch {
			case ctrl != nil && ctrl.Kind == "DaemonSet":
				continue
			case p.Status.Phase == core.PodSucceeded || p.Status.Phase == core.PodFailed:
				continue
			case ctrl == nil:
				problems = append(problems, p.Namespace+"/"+p.Name)
				continue
			}
			pods = append(pods, p)
		}
		token = list.Continue
		if token == "" {
			sort.Slice(pods, func(i, j int) bool { return pods[i].Namespace+pods[i].Name < pods[j].Namespace+pods[j].Name })
			return pods, problems, nil
		}
	}
}

func drain(ctx context.Context, c kubernetes.Interface, node string, timeout time.Duration, o *Options) error {
	n, err := c.CoreV1().Nodes().Get(ctx, node, meta.GetOptions{})
	if err != nil {
		return fmt.Errorf("node %s: %w", node, err)
	}
	if !n.Spec.Unschedulable && !o.DryRun {
		return fmt.Errorf("node %s is not cordoned; cordon it first so evicted pods are not rescheduled onto it", node)
	}
	pods, problems, err := evictable(ctx, c, node)
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		return fmt.Errorf("node %s runs pods without a controller that a drain would lose: %s", node, strings.Join(limit(problems, 10), ", "))
	}
	o.say("  draining %s: %d pod(s) to evict (eviction API; PodDisruptionBudgets are respected)", node, len(pods))
	deadline := time.Now().Add(timeout)
	pending := pods
	backoff := 2 * time.Second
	for len(pending) > 0 {
		var blocked []core.Pod
		for _, p := range pending {
			ev := &policy.Eviction{ObjectMeta: meta.ObjectMeta{Name: p.Name, Namespace: p.Namespace}, DeleteOptions: &meta.DeleteOptions{DryRun: dry(o)}}
			err := c.CoreV1().Pods(p.Namespace).EvictV1(ctx, ev)
			switch {
			case err == nil, apierrors.IsNotFound(err):
			case apierrors.IsTooManyRequests(err):
				blocked = append(blocked, p) // a PodDisruptionBudget does not allow it yet
			default:
				return fmt.Errorf("evict %s/%s: %w", p.Namespace, p.Name, err)
			}
		}
		if len(blocked) == 0 {
			break
		}
		if o.DryRun {
			o.say("  %d pod(s) are currently blocked by a PodDisruptionBudget; a real drain would wait for them", len(blocked))
			return nil
		}
		if time.Now().Add(backoff).After(deadline) {
			names := make([]string, 0, len(blocked))
			for _, p := range blocked {
				names = append(names, p.Namespace+"/"+p.Name)
			}
			return fmt.Errorf("drain of %s timed out; PodDisruptionBudgets still block: %s (fix the cause; apply never forces)", node, strings.Join(limit(names, 10), ", "))
		}
		o.sleep(backoff)
		if backoff < 15*time.Second {
			backoff *= 2
		}
		pending = blocked
	}
	if o.DryRun {
		return nil
	}
	// Wait for the evicted pods to be gone.
	poll := o.Poll
	if poll == 0 {
		poll = 2 * time.Second
	}
	for {
		left, _, err := evictable(ctx, c, node)
		if err != nil {
			return err
		}
		if len(left) == 0 {
			o.say("  drained %s", node)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("drain of %s timed out with %d pod(s) still present", node, len(left))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		o.sleep(poll)
	}
}

func limit(l []string, n int) []string {
	if len(l) <= n {
		return l
	}
	return append(append([]string{}, l[:n]...), fmt.Sprintf("... and %d more", len(l)-n))
}
