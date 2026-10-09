package cli

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/amitpatole/cgroupguard/internal/detect"
	"github.com/amitpatole/cgroupguard/internal/model"
	"github.com/amitpatole/cgroupguard/internal/rules"
	"github.com/amitpatole/cgroupguard/internal/scan"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// scanOpts holds the flags shared by every command that starts from a scan
// (check and fix). Scanning is read-only unless --probe is given explicitly.
type scanOpts struct {
	input, kubeconfig, contextName, namespace, image, path, target, prometheusURL, workloadNamespace string
	local, probe, nodesOnly, kubeletMetrics, nodeEvents                                              bool
	timeout                                                                                          time.Duration
}

func (o *scanOpts) bind(f *pflag.FlagSet) {
	f.StringVar(&o.input, "input", "", "Offline snapshot JSON (schemaVersion 1)")
	f.StringVarP(&o.target, "target-version", "t", rules.DefaultTarget, "Planned Kubernetes upgrade target; scales finding severity (for example 1.35)")
	f.BoolVar(&o.kubeletMetrics, "kubelet-metrics", false, "Read kubelet_cgroup_version through the API-server node proxy (needs nodes/proxy; opt-in)")
	f.BoolVar(&o.nodeEvents, "node-events", false, "Also read kubelet CgroupV1 Warning Events (needs list on events; absence is never evidence of v2)")
	f.StringVar(&o.prometheusURL, "prometheus-url", "", "Read kubelet_cgroup_version from an existing Prometheus (http/https URL)")
	f.BoolVar(&o.nodesOnly, "nodes-only", false, "Skip the live workload inventory and read node information only")
	f.StringVar(&o.workloadNamespace, "workload-namespace", "", "Only scan workloads in this namespace (default: all)")
	f.BoolVar(&o.local, "local", false, "Inspect this Linux mount namespace; no cluster connection")
	f.StringVar(&o.path, "path", "/sys/fs/cgroup", "Cgroup root for --local")
	f.StringVar(&o.kubeconfig, "kubeconfig", "", "Kubeconfig path; defaults to standard client-go loading")
	f.StringVar(&o.contextName, "context", "", "Kubeconfig context")
	f.BoolVar(&o.probe, "probe", false, "Explicitly authorize temporary hostPath probe pod creation/deletion")
	f.StringVar(&o.namespace, "namespace", "", "Existing namespace for probe pods")
	f.StringVar(&o.image, "probe-image", "", "Trusted cgroupguard image pinned by digest")
	f.DurationVar(&o.timeout, "timeout", 5*time.Minute, "Overall live scan timeout")
}

// scan validates the flags, collects evidence and evaluates the rules.
func (o *scanOpts) scan(cmd *cobra.Command) (model.Report, error) {
	target, e := rules.ParseTarget(o.target)
	if e != nil {
		return model.Report{}, e
	}
	changed := cmd.Flags().Changed
	switch {
	case (o.kubeletMetrics || o.nodeEvents || o.prometheusURL != "") && (o.input != "" || o.local):
		return model.Report{}, fmt.Errorf("metrics options require a live cluster scan")
	case o.timeout <= 0:
		return model.Report{}, fmt.Errorf("timeout must be positive")
	case o.probe && (o.input != "" || o.local):
		return model.Report{}, fmt.Errorf("--probe requires a live cluster scan")
	case o.probe && (o.image == "" || o.namespace == ""):
		return model.Report{}, fmt.Errorf("--probe requires --probe-image and --namespace")
	case !o.probe && (changed("probe-image") || changed("namespace")):
		return model.Report{}, fmt.Errorf("probe options require --probe")
	case (o.local || o.input != "") && (changed("context") || changed("kubeconfig")):
		return model.Report{}, fmt.Errorf("cluster connection options cannot be used with local/offline scans")
	case !o.local && changed("path"):
		return model.Report{}, fmt.Errorf("--path requires --local")
	case (o.local || o.input != "" || o.nodesOnly) && changed("workload-namespace"):
		return model.Report{}, fmt.Errorf("--workload-namespace requires live workload scanning")
	case (o.local || o.input != "") && o.nodesOnly:
		return model.Report{}, fmt.Errorf("--nodes-only applies only to live cluster scans")
	}
	var nodes []model.Node
	var workloads []model.Workload
	var limits []string
	switch {
	case o.input != "":
		f, e := openSnapshot(o.input)
		if e != nil {
			return model.Report{}, e
		}
		defer f.Close()
		s, e := ReadSnapshot(f)
		if e != nil {
			return model.Report{}, e
		}
		nodes, workloads = s.Nodes, s.Workloads
	case o.local:
		mode, evidence, e := detect.Local(o.path)
		if e != nil {
			return model.Report{}, e
		}
		name, e := os.Hostname()
		if e != nil {
			return model.Report{}, e
		}
		nodes = []model.Node{{Name: name, OS: runtime.GOOS, Provider: "local", Cgroup: mode, Evidence: evidence + "; current mount namespace only"}}
	default:
		ctx, cancel := context.WithTimeout(cmd.Context(), o.timeout)
		defer cancel()
		client, e := detect.Client(o.kubeconfig, o.contextName)
		if e != nil {
			return model.Report{}, e
		}
		ev, e := scan.Collect(ctx, client, scan.Options{Target: target, Workloads: !o.nodesOnly, WorkloadNamespace: o.workloadNamespace, PrometheusURL: o.prometheusURL, KubeletMetrics: o.kubeletMetrics, NodeEvents: o.nodeEvents})
		if e != nil {
			return model.Report{}, e
		}
		nodes, workloads, limits = ev.Nodes, ev.Workloads, ev.Limits
		if o.probe {
			for i := range nodes {
				if e = detect.Probe(ctx, client, &nodes[i], o.namespace, o.image); e != nil {
					return model.Report{}, fmt.Errorf("node %s: %w", nodes[i].Name, e)
				}
			}
		}
	}
	return scan.Evaluate(scan.Evidence{Nodes: nodes, Workloads: workloads, Limits: limits}, target, o.workloadNamespace)
}
