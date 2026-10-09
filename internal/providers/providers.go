package providers

import "strings"

// These are platform hints, never evidence for a cgroup version.
func Detect(labels map[string]string, id string) string {
	if _, ok := labels["cloud.google.com/gke-nodepool"]; ok {
		return "gke"
	}
	if _, ok := labels["eks.amazonaws.com/nodegroup"]; ok {
		return "eks"
	}
	if _, ok := labels["kubernetes.azure.com/cluster"]; ok {
		return "aks"
	}
	if _, ok := labels["node.openshift.io/os_id"]; ok {
		return "openshift"
	}
	if _, ok := labels["node.kubernetes.io/instance-type"]; ok && labels["node.kubernetes.io/instance-type"] == "k3s" {
		return "k3s"
	}
	if strings.HasPrefix(id, "gce://") {
		return "gcp-unconfirmed"
	}
	if strings.HasPrefix(id, "aws://") {
		return "aws-unconfirmed"
	}
	if strings.HasPrefix(id, "azure://") {
		return "azure-unconfirmed"
	}
	return "unknown"
}

// poolLabels are well-known node-pool labels, in priority order. A pool name is
// grouping metadata only; it is never evidence of a cgroup version.
var poolLabels = []string{"cloud.google.com/gke-nodepool", "eks.amazonaws.com/nodegroup", "kubernetes.azure.com/agentpool", "karpenter.sh/nodepool", "karpenter.k8s.aws/ec2nodeclass"}

// Pool returns the node pool or node group name from labels, or "".
func Pool(labels map[string]string) string {
	for _, l := range poolLabels {
		if v := labels[l]; v != "" {
			return v
		}
	}
	return ""
}
