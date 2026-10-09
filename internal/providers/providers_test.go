package providers

import "testing"

func TestProviderIDIsNotManagedServiceProof(t *testing.T) {
	if Detect(nil, "aws:///zone/id") != "aws-unconfirmed" {
		t.Fatal("AWS VM must not imply EKS")
	}
	if Detect(nil, "") != "unknown" {
		t.Fatal("generic cluster must not imply kubeadm")
	}
}

func TestPool(t *testing.T) {
	if Pool(map[string]string{"cloud.google.com/gke-nodepool": "p"}) != "p" || Pool(map[string]string{"kubernetes.azure.com/agentpool": "q"}) != "q" || Pool(nil) != "" {
		t.Fatal("pool labels")
	}
}
