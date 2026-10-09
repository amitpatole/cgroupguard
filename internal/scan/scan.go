// Package scan is the one place that collects live evidence and evaluates it. The CLI and
// the in-cluster controller both use it, so they cannot drift apart.
package scan

import (
	"context"
	"fmt"
	"time"

	"github.com/amitpatole/cgroupguard/internal/detect"
	"github.com/amitpatole/cgroupguard/internal/model"
	"github.com/amitpatole/cgroupguard/internal/rules"
	"k8s.io/client-go/kubernetes"
)

// Options select the evidence sources. Every source beyond the node and workload
// inventory is opt-in because it needs extra permission or network access.
type Options struct {
	Target            string
	Workloads         bool
	WorkloadNamespace string
	PrometheusURL     string // operator-controlled, never taken from a custom resource
	KubeletMetrics    bool   // needs nodes/proxy
	NodeEvents        bool   // needs list on events
}

// Evidence is what Collect gathers before the rules run.
type Evidence struct {
	Nodes     []model.Node
	Workloads []model.Workload
	Limits    []string
}

// Collect reads the cluster. Permission failures on required resources are errors;
// failures of optional sources become limitations.
func Collect(ctx context.Context, c kubernetes.Interface, o Options) (Evidence, error) {
	var ev Evidence
	var err error
	if ev.Nodes, err = detect.Inventory(ctx, c); err != nil {
		return ev, err
	}
	if o.Workloads {
		var l []string
		if ev.Workloads, l, err = detect.Workloads(ctx, c, o.WorkloadNamespace); err != nil {
			return ev, err
		}
		ev.Limits = append(ev.Limits, l...)
	}
	if o.PrometheusURL != "" {
		l, err := detect.PrometheusCgroup(ctx, detect.HTTPClient(), o.PrometheusURL, ev.Nodes, time.Now())
		if err != nil {
			return ev, fmt.Errorf("prometheus: %w", err)
		}
		ev.Limits = append(ev.Limits, l...)
	}
	if o.KubeletMetrics {
		ev.Limits = append(ev.Limits, detect.ApplyKubeletMetrics(ctx, ev.Nodes, detect.NodeProxyFetcher(c))...)
	}
	if o.NodeEvents {
		l, err := detect.ApplyNodeEvents(ctx, c, ev.Nodes)
		if err != nil {
			l = []string{"Kubelet CgroupV1 Events could not be read (" + err.Error() + "); node modes were not changed by events"}
		}
		ev.Limits = append(ev.Limits, l...)
	}
	return ev, nil
}

// Evaluate applies the rules to collected or supplied evidence.
func Evaluate(ev Evidence, target, workloadNamespace string) (model.Report, error) {
	set, err := rules.Load()
	if err != nil {
		return model.Report{}, err
	}
	r, err := rules.EvaluateFor(set, ev.Nodes, target)
	if err != nil {
		return r, err
	}
	if err = rules.AddWorkloads(&r, ev.Workloads); err != nil {
		return r, err
	}
	r.Limitations = append(r.Limitations, ev.Limits...)
	if workloadNamespace != "" {
		r.Limitations = append(r.Limitations, "Workload inventory is restricted to namespace "+workloadNamespace)
	}
	return r, nil
}
