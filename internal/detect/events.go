package detect

import (
	"context"
	"fmt"
	"time"

	"github.com/amitpatole/cgroupguard/internal/model"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// The kubelet records a Warning Event with reason "CgroupV1" on its Node when it
// runs on cgroup v1 (pkg/kubelet/kubelet_linux.go cgroupVersionCheck). Events are
// short-lived (the API server keeps them about an hour by default), so absence of
// an event is never evidence of v2, and a present event can be stale.
const eventReason = "CgroupV1"

// ApplyNodeEvents upgrades nodes whose mode is unknown to v1 when a CgroupV1
// event exists. Nodes with other evidence are never overridden; a disagreement
// is reported as a limitation.
func ApplyNodeEvents(ctx context.Context, c kubernetes.Interface, nodes []model.Node) ([]string, error) {
	seen := map[string]core.Event{}
	token := ""
	for {
		list, e := c.CoreV1().Events("").List(ctx, meta.ListOptions{FieldSelector: "reason=" + eventReason + ",involvedObject.kind=Node", Limit: 500, Continue: token})
		if e != nil {
			return nil, fmt.Errorf("list Events: %w", e)
		}
		for _, ev := range list.Items {
			if ev.Reason != eventReason || ev.InvolvedObject.Kind != "Node" {
				continue // field selectors are advisory for some servers and fakes
			}
			if prev, ok := seen[ev.InvolvedObject.Name]; !ok || eventTime(ev).After(eventTime(prev)) {
				seen[ev.InvolvedObject.Name] = ev
			}
		}
		token = list.Continue
		if token == "" {
			break
		}
	}
	var limits []string
	for i := range nodes {
		n := &nodes[i]
		ev, ok := seen[n.Name]
		if !ok || n.OS != "linux" {
			continue
		}
		when := eventTime(ev).UTC().Format(time.RFC3339)
		switch n.Cgroup {
		case "unknown":
			n.Cgroup = "v1"
			n.Evidence = fmt.Sprintf("kubelet Event reason=%s at %s (events expire; may be stale after a migration)", eventReason, when)
		case "v1", "hybrid":
		default:
			limits = append(limits, fmt.Sprintf("Node %s has a %s event at %s that conflicts with %s evidence; the event may be stale", n.Name, eventReason, when, n.Cgroup))
		}
	}
	return limits, nil
}

func eventTime(e core.Event) time.Time {
	switch {
	case !e.LastTimestamp.IsZero():
		return e.LastTimestamp.Time
	case !e.EventTime.IsZero():
		return e.EventTime.Time
	}
	return e.CreationTimestamp.Time
}
