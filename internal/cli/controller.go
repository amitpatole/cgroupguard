package cli

import (
	"github.com/amitpatole/cgroupguard/internal/controller"
	"github.com/amitpatole/cgroupguard/internal/scan"
	"github.com/spf13/cobra"
)

func controllerCommand() *cobra.Command {
	var o controller.Options
	var promURL string
	var nodeEvents, kubeletMetrics bool
	cmd := &cobra.Command{Use: "controller", Short: "Run the optional in-cluster controller (CgroupReadiness resources, metrics, Events)", Args: cobra.NoArgs,
		Long: "Runs inside a cluster. It reads nodes and workload metadata, writes only the status of CgroupReadiness resources and Events, and serves Prometheus metrics. Evidence sources beyond the inventory (--prometheus-url, --node-events, --kubelet-metrics) are chosen here by the operator and cannot be selected through a custom resource.",
		RunE: func(cmd *cobra.Command, args []string) error {
			o.Config.Scan = scan.Options{PrometheusURL: promURL, NodeEvents: nodeEvents, KubeletMetrics: kubeletMetrics}
			return controller.Run(cmd.Context(), o)
		}}
	f := cmd.Flags()
	f.StringVar(&o.MetricsAddr, "metrics-bind-address", ":8080", "Address of the Prometheus metrics endpoint")
	f.StringVar(&o.ProbeAddr, "health-probe-bind-address", ":8081", "Address of the /healthz and /readyz endpoints")
	f.BoolVar(&o.LeaderElect, "leader-elect", true, "Use leader election so only one replica scans")
	f.StringVar(&o.LeaderNamespace, "leader-election-namespace", "", "Namespace for the leader election lease (default: the controller's own)")
	f.StringVar(&promURL, "prometheus-url", "", "Read kubelet_cgroup_version from this Prometheus (operator-controlled)")
	f.BoolVar(&nodeEvents, "node-events", false, "Also read kubelet CgroupV1 Events (needs list on events)")
	f.BoolVar(&kubeletMetrics, "kubelet-metrics", false, "Read kubelet metrics through the API-server node proxy (needs nodes/proxy; high risk)")
	return cmd
}
