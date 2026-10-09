package detect

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/amitpatole/cgroupguard/internal/model"
	"github.com/amitpatole/cgroupguard/internal/providers"
	"io"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"regexp"
	"time"
)

func Client(kubeconfig, contextName string) (kubernetes.Interface, error) {
	loading := clientcmd.NewDefaultClientConfigLoadingRules()
	loading.ExplicitPath = kubeconfig
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loading, &clientcmd.ConfigOverrides{CurrentContext: contextName}).ClientConfig()
	if err != nil {
		return nil, err
	}
	config.Timeout = 30 * time.Second
	return kubernetes.NewForConfig(config)
}

func Inventory(ctx context.Context, c kubernetes.Interface) ([]model.Node, error) {
	nodes := []model.Node{}
	token := ""
	for {
		list, err := c.CoreV1().Nodes().List(ctx, meta.ListOptions{Limit: 500, Continue: token})
		if err != nil {
			return nil, err
		}
		for _, n := range list.Items {
			i := n.Status.NodeInfo
			nodes = append(nodes, model.Node{Name: n.Name, UID: string(n.UID), OS: i.OperatingSystem, OSImage: i.OSImage, Kernel: i.KernelVersion, Kubelet: i.KubeletVersion, Runtime: i.ContainerRuntimeVersion, Provider: providers.Detect(n.Labels, n.Spec.ProviderID), Pool: providers.Pool(n.Labels), Arch: i.Architecture, Instance: n.Labels["node.kubernetes.io/instance-type"], Cgroup: "unknown", Evidence: "Kubernetes Node API inventory only; cgroup filesystem not probed"})
		}
		token = list.Continue
		if token == "" {
			break
		}
	}
	return nodes, nil
}

func ProbePod(node, namespace, image string) *core.Pod {
	no, yes := false, true
	uid := int64(65532)
	deadline := int64(90)
	return &core.Pod{ObjectMeta: meta.ObjectMeta{GenerateName: "cgroupguard-probe-", Namespace: namespace, Labels: map[string]string{"app.kubernetes.io/name": "cgroupguard-probe"}}, Spec: core.PodSpec{
		NodeName: node, RestartPolicy: core.RestartPolicyNever, ActiveDeadlineSeconds: &deadline, AutomountServiceAccountToken: &no,
		SecurityContext: &core.PodSecurityContext{RunAsNonRoot: &yes, RunAsUser: &uid, RunAsGroup: &uid, SeccompProfile: &core.SeccompProfile{Type: core.SeccompProfileTypeRuntimeDefault}},
		Tolerations:     []core.Toleration{{Operator: core.TolerationOpExists}},
		Containers:      []core.Container{{Name: "probe", Image: image, ImagePullPolicy: core.PullIfNotPresent, Command: []string{"/cgroupguard", "probe", "--path", "/host-cgroup"}, SecurityContext: &core.SecurityContext{AllowPrivilegeEscalation: &no, ReadOnlyRootFilesystem: &yes, Capabilities: &core.Capabilities{Drop: []core.Capability{"ALL"}}}, Resources: core.ResourceRequirements{Requests: core.ResourceList{core.ResourceCPU: resource.MustParse("10m"), core.ResourceMemory: resource.MustParse("16Mi")}, Limits: core.ResourceList{core.ResourceCPU: resource.MustParse("100m"), core.ResourceMemory: resource.MustParse("64Mi")}}, VolumeMounts: []core.VolumeMount{{Name: "cgroup", MountPath: "/host-cgroup", ReadOnly: true}}}},
		Volumes:         []core.Volume{{Name: "cgroup", VolumeSource: core.VolumeSource{HostPath: &core.HostPathVolumeSource{Path: "/sys/fs/cgroup", Type: ptr(core.HostPathDirectory)}}}},
	}}
}
func ptr[T any](v T) *T { return &v }

// Probe creates only a short-lived pod, then removes exactly that pod by UID.
// It never changes kubelet, node scheduling, workloads or RBAC.
func Probe(ctx context.Context, c kubernetes.Interface, n *model.Node, namespace, image string) (err error) {
	if n.OS != "linux" {
		return nil
	}
	if !regexp.MustCompile(`^[^\s@]+@sha256:[0-9a-f]{64}$`).MatchString(image) {
		return fmt.Errorf("probe image must be pinned by sha256 digest")
	}
	pods := c.CoreV1().Pods(namespace)
	p, err := pods.Create(ctx, ProbePod(n.Name, namespace, image), meta.CreateOptions{})
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		e := pods.Delete(cleanup, p.Name, meta.DeleteOptions{Preconditions: &meta.Preconditions{UID: &p.UID}})
		if e != nil {
			err = fmt.Errorf("probe result: %v; cleanup failed for pod %s/%s: %w", err, namespace, p.Name, e)
		}
	}()
	wait, cancel := context.WithTimeout(ctx, 100*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		current, e := pods.Get(wait, p.Name, meta.GetOptions{})
		if e != nil {
			return e
		}
		if current.Status.Phase == core.PodFailed {
			return fmt.Errorf("probe pod %s/%s failed", namespace, p.Name)
		}
		if current.Status.Phase == core.PodSucceeded {
			break
		}
		select {
		case <-wait.Done():
			return fmt.Errorf("probe pod %s/%s: %w", namespace, p.Name, wait.Err())
		case <-ticker.C:
		}
	}
	stream, e := pods.GetLogs(p.Name, &core.PodLogOptions{Container: "probe", LimitBytes: ptr(int64(8192))}).Stream(wait)
	if e != nil {
		return e
	}
	defer stream.Close()
	var result struct {
		Cgroup   string `json:"cgroup"`
		Evidence string `json:"evidence"`
	}
	decoder := json.NewDecoder(io.LimitReader(stream, 8193))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&result); e != nil {
		return fmt.Errorf("invalid probe result: %w", e)
	}
	if result.Cgroup != "v1" && result.Cgroup != "v2" && result.Cgroup != "hybrid" && result.Cgroup != "unknown" {
		return fmt.Errorf("invalid probe mode")
	}
	if result.Evidence == "" {
		return fmt.Errorf("missing probe evidence")
	}
	mergeProbe(n, result.Cgroup, result.Evidence, string(p.UID))
	return nil
}

// mergeProbe records probe evidence. When metrics and probe disagree the node is
// reported unknown with both pieces of evidence rather than trusting either.
func mergeProbe(n *model.Node, cgroup, evidence, uid string) {
	if n.Cgroup != "unknown" && cgroup != "unknown" && n.Cgroup != cgroup {
		n.Evidence = "conflicting evidence: " + n.Evidence + " vs probe " + evidence
		n.Cgroup = "unknown"
		return
	}
	n.Cgroup = cgroup
	n.Evidence = evidence + "; pod UID=" + uid
}
