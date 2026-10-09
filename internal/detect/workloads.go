package detect

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/amitpatole/cgroupguard/internal/model"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

var CgroupPaths = []string{"/sys/fs/cgroup/memory/", "/sys/fs/cgroup/cpu/", "/sys/fs/cgroup/cpuacct/", "/sys/fs/cgroup/cpuset/", "/sys/fs/cgroup/cpu,cpuacct/"}

// PathSignals emits only allowlisted path prefixes, never the surrounding input.
func PathSignals(values []string) []string {
	found := []string{}
	for _, path := range CgroupPaths {
		pattern := regexp.MustCompile(regexp.QuoteMeta(strings.TrimSuffix(path, "/")) + `(?:/|$|[\s"'])`)
		for _, value := range values {
			if pattern.MatchString(value) {
				found = append(found, path)
				break
			}
		}
	}
	return found
}

func projectWith(namespace, kind, name string, spec core.PodSpec, decorate func(*model.Workload)) []model.Workload {
	result := []model.Workload{}
	add := func(c core.Container, typ string) {
		values := append(append([]string{}, c.Command...), c.Args...)
		for _, m := range c.VolumeMounts {
			values = append(values, m.MountPath)
		}
		w := model.Workload{Namespace: namespace, Kind: kind, Name: name, Container: c.Name, ContainerType: typ, Image: c.Image, PathSignals: PathSignals(values)}
		decorate(&w)
		result = append(result, w)
	}
	for _, c := range spec.Containers {
		add(c, "regular")
	}
	for _, c := range spec.InitContainers {
		add(c, "init")
	}
	for _, c := range spec.EphemeralContainers {
		add(core.Container{Name: c.Name, Image: c.Image, Command: c.Command, Args: c.Args, VolumeMounts: c.VolumeMounts}, "ephemeral")
	}
	return result
}

// podInfo carries what Workloads needs to attribute a Pod to its controller.
type podInfo struct {
	namespace, name, node, ownerKind, ownerName string
	spec                                        core.PodSpec
}

// controller is one listed workload template plus the facts used for blast radius.
type controller struct {
	namespace, kind, name string
	spec                  core.PodSpec
	labels                map[string]string
	replicas              *int32
	stateful              bool
	jobOwnerCronJob       string // for Jobs created by a CronJob
	placements            map[string]int
}

func hasPVC(spec core.PodSpec) bool {
	for _, v := range spec.Volumes {
		if v.PersistentVolumeClaim != nil || v.Ephemeral != nil {
			return true
		}
	}
	return false
}

func controllerOwner(refs []meta.OwnerReference) (string, string) {
	for _, r := range refs {
		if r.Controller != nil && *r.Controller {
			return r.Kind, r.Name
		}
	}
	return "", ""
}

// Workloads lists Pods and the requested controller templates (paginated
// independently) and returns one entry per container. Pods owned by a listed
// controller are folded into it (their node placement is counted, the Pod is not
// reported twice). The second return value lists non-fatal limitations, e.g. an
// unreadable PodDisruptionBudget list. Permission errors on workload resources
// fail the scan, never masquerade as an empty inventory.
func Workloads(ctx context.Context, c kubernetes.Interface, namespace string) ([]model.Workload, []string, error) {
	var pods []podInfo
	ctrls := map[string]*controller{}
	key := func(ns, kind, name string) string { return ns + "/" + kind + "/" + name }
	total := 0
	for _, kind := range []string{"Pod", "Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob"} {
		token := ""
		for {
			opts := meta.ListOptions{Limit: 500, Continue: token}
			next := ""
			add := func(ns, name string, spec core.PodSpec, tmpl map[string]string, replicas *int32, stateful bool) {
				ctrls[key(ns, kind, name)] = &controller{namespace: ns, kind: kind, name: name, spec: spec, labels: tmpl, replicas: replicas, stateful: stateful || hasPVC(spec), placements: map[string]int{}}
				total += len(spec.Containers) + len(spec.InitContainers)
			}
			switch kind {
			case "Pod":
				list, e := c.CoreV1().Pods(namespace).List(ctx, opts)
				if e != nil {
					return nil, nil, fmt.Errorf("list Pods: %w", e)
				}
				next = list.Continue
				for _, o := range list.Items {
					k, n := controllerOwner(o.OwnerReferences)
					pods = append(pods, podInfo{o.Namespace, o.Name, o.Spec.NodeName, k, n, o.Spec})
					total += len(o.Spec.Containers) + len(o.Spec.InitContainers) + len(o.Spec.EphemeralContainers)
				}
			case "Deployment":
				list, e := c.AppsV1().Deployments(namespace).List(ctx, opts)
				if e != nil {
					return nil, nil, fmt.Errorf("list Deployments: %w", e)
				}
				next = list.Continue
				for _, o := range list.Items {
					r := o.Spec.Replicas
					if r == nil {
						one := int32(1)
						r = &one
					}
					add(o.Namespace, o.Name, o.Spec.Template.Spec, o.Spec.Template.Labels, r, false)
				}
			case "StatefulSet":
				list, e := c.AppsV1().StatefulSets(namespace).List(ctx, opts)
				if e != nil {
					return nil, nil, fmt.Errorf("list StatefulSets: %w", e)
				}
				next = list.Continue
				for _, o := range list.Items {
					r := o.Spec.Replicas
					if r == nil {
						one := int32(1)
						r = &one
					}
					add(o.Namespace, o.Name, o.Spec.Template.Spec, o.Spec.Template.Labels, r, len(o.Spec.VolumeClaimTemplates) > 0)
				}
			case "DaemonSet":
				list, e := c.AppsV1().DaemonSets(namespace).List(ctx, opts)
				if e != nil {
					return nil, nil, fmt.Errorf("list DaemonSets: %w", e)
				}
				next = list.Continue
				for _, o := range list.Items {
					r := o.Status.DesiredNumberScheduled
					add(o.Namespace, o.Name, o.Spec.Template.Spec, o.Spec.Template.Labels, &r, false)
				}
			case "Job":
				list, e := c.BatchV1().Jobs(namespace).List(ctx, opts)
				if e != nil {
					return nil, nil, fmt.Errorf("list Jobs: %w", e)
				}
				next = list.Continue
				for _, o := range list.Items {
					one := int32(1)
					if o.Spec.Parallelism != nil {
						one = *o.Spec.Parallelism
					}
					add(o.Namespace, o.Name, o.Spec.Template.Spec, o.Spec.Template.Labels, &one, false)
					if k, n := controllerOwner(o.OwnerReferences); k == "CronJob" {
						ctrls[key(o.Namespace, kind, o.Name)].jobOwnerCronJob = n
					}
				}
			case "CronJob":
				list, e := c.BatchV1().CronJobs(namespace).List(ctx, opts)
				if e != nil {
					return nil, nil, fmt.Errorf("list CronJobs: %w", e)
				}
				next = list.Continue
				for _, o := range list.Items {
					one := int32(1)
					add(o.Namespace, o.Name, o.Spec.JobTemplate.Spec.Template.Spec, o.Spec.JobTemplate.Spec.Template.Labels, &one, false)
				}
			}
			if total > 20000 {
				return nil, nil, fmt.Errorf("workload inventory exceeds 20000 containers; narrow --workload-namespace")
			}
			if next == "" {
				break
			}
			if next == token {
				return nil, nil, fmt.Errorf("repeated continuation token for %s", kind)
			}
			token = next
		}
	}
	// Jobs created by a CronJob fold into it; the CronJob template is reported once.
	for k, j := range ctrls {
		if j.kind == "Job" && j.jobOwnerCronJob != "" {
			if parent, ok := ctrls[key(j.namespace, "CronJob", j.jobOwnerCronJob)]; ok {
				_ = parent
				continue
			}
			_ = k
		}
	}
	resolve := func(p podInfo) (*controller, bool) {
		switch p.ownerKind {
		case "ReplicaSet":
			// ReplicaSets are not listed (less RBAC); accept the Deployment only if
			// it exists and the ReplicaSet name is "<deployment>-<hash>".
			if i := strings.LastIndex(p.ownerName, "-"); i > 0 {
				if d, ok := ctrls[key(p.namespace, "Deployment", p.ownerName[:i])]; ok {
					return d, true
				}
			}
		case "StatefulSet", "DaemonSet":
			if d, ok := ctrls[key(p.namespace, p.ownerKind, p.ownerName)]; ok {
				return d, true
			}
		case "Job":
			if j, ok := ctrls[key(p.namespace, "Job", p.ownerName)]; ok {
				if j.jobOwnerCronJob != "" {
					if cj, ok := ctrls[key(p.namespace, "CronJob", j.jobOwnerCronJob)]; ok {
						return cj, true
					}
				}
				return j, true
			}
		}
		return nil, false
	}
	var standalone []podInfo
	for _, p := range pods {
		if ctrl, ok := resolve(p); ok {
			if p.node != "" {
				ctrl.placements[p.node]++
			}
			continue
		}
		standalone = append(standalone, p)
	}
	// Jobs owned by an existing CronJob are not reported separately.
	for k, j := range ctrls {
		if j.kind == "Job" && j.jobOwnerCronJob != "" {
			if _, ok := ctrls[key(j.namespace, "CronJob", j.jobOwnerCronJob)]; ok {
				delete(ctrls, k)
			}
		}
	}
	limits := []string{}
	pdbs, e := listPDBs(ctx, c, namespace)
	if e != nil {
		limits = append(limits, "PodDisruptionBudgets could not be read ("+e.Error()+"); recoverability is unknown. Grant list on policy/poddisruptionbudgets (deploy/workload-rbac.yaml) to measure it.")
	}
	result := []model.Workload{}
	for _, ctrl := range ctrls {
		pdb := "unknown"
		if e == nil {
			pdb = "none"
			for _, b := range pdbs {
				if b.namespace == ctrl.namespace && b.selector != nil && b.selector.Matches(labels.Set(ctrl.labels)) {
					pdb = "present"
					break
				}
			}
		}
		result = append(result, projectWith(ctrl.namespace, ctrl.kind, ctrl.name, ctrl.spec, func(w *model.Workload) {
			w.Replicas = ctrl.replicas
			w.Stateful = ctrl.stateful
			w.PDB = pdb
			if len(ctrl.placements) > 0 {
				w.Placements = ctrl.placements
			}
		})...)
	}
	for _, p := range standalone {
		one := int32(1)
		pdb := "unknown"
		if e == nil {
			pdb = "none"
		}
		result = append(result, projectWith(p.namespace, "Pod", p.name, p.spec, func(w *model.Workload) {
			w.Replicas = &one
			w.Stateful = hasPVC(p.spec)
			w.PDB = pdb
			if p.node != "" {
				w.Placements = map[string]int{p.node: 1}
			}
		})...)
	}
	sort.Slice(result, func(i, j int) bool { return WorkloadKey(result[i]) < WorkloadKey(result[j]) })
	return result, limits, nil
}

type pdbRef struct {
	namespace string
	selector  labels.Selector
}

func listPDBs(ctx context.Context, c kubernetes.Interface, namespace string) ([]pdbRef, error) {
	var out []pdbRef
	token := ""
	for {
		list, e := c.PolicyV1().PodDisruptionBudgets(namespace).List(ctx, meta.ListOptions{Limit: 500, Continue: token})
		if e != nil {
			return nil, e
		}
		for _, b := range list.Items {
			sel, e := meta.LabelSelectorAsSelector(b.Spec.Selector)
			if e != nil {
				return nil, e
			}
			out = append(out, pdbRef{b.Namespace, sel})
		}
		token = list.Continue
		if token == "" {
			return out, nil
		}
	}
}

func WorkloadKey(w model.Workload) string {
	return w.Namespace + "/" + w.Kind + "/" + w.Name + "/" + w.ContainerType + "/" + w.Container
}
