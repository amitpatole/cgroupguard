package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/amitpatole/cgroupguard/internal/scan"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GVK identifies the custom resource.
var GVK = schema.GroupVersionKind{Group: Group, Version: Version, Kind: Kind}

// Emitter raises Kubernetes Events about a resource (events.k8s.io API).
type Emitter interface {
	Eventf(obj runtime.Object, eventType, reason, format string, args ...any)
}

// EventsAPI adapts a controller-runtime events recorder to Emitter.
type EventsAPI struct{ R events.EventRecorder }

func (e EventsAPI) Eventf(obj runtime.Object, eventType, reason, format string, args ...any) {
	e.R.Eventf(obj, nil, eventType, reason, reason, format, args...)
}

// Scanner is what the reconciler needs from the scan package; tests replace it.
type Scanner interface {
	Run(ctx context.Context, sp Spec) (scan.Evidence, error)
}

// Config holds operator-controlled settings. None of these can be changed through a
// CgroupReadiness resource.
type Config struct {
	Scan          scan.Options // PrometheusURL, KubeletMetrics, NodeEvents
	ScanTimeout   time.Duration
	RetryInterval time.Duration
}

type Reconciler struct {
	Client   client.Client
	Kube     kubernetes.Interface
	Recorder Emitter
	Cfg      Config
	Metrics  *Metrics
	Scanner  Scanner
	Now      func() time.Time
}

type liveScanner struct {
	kube kubernetes.Interface
	cfg  Config
}

func NewLiveScanner(kube kubernetes.Interface, cfg Config) Scanner { return liveScanner{kube, cfg} }

func (l liveScanner) Run(ctx context.Context, sp Spec) (scan.Evidence, error) {
	o := l.cfg.Scan
	o.Target, o.Workloads, o.WorkloadNamespace = sp.Target, sp.IncludeWorkloads, sp.WorkloadNamespace
	return scan.Collect(ctx, l.kube, o)
}

func (r *Reconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Reconciler) setStatus(ctx context.Context, name string, st map[string]any) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cur := &unstructured.Unstructured{}
		cur.SetGroupVersionKind(GVK)
		if err := r.Client.Get(ctx, types.NamespacedName{Name: name}, cur); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(cur.Object, st, "status"); err != nil {
			return err
		}
		return r.Client.Status().Update(ctx, cur)
	})
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(GVK)
	if err := r.Client.Get(ctx, req.NamespacedName, obj); err != nil {
		if apierrors.IsNotFound(err) {
			r.Metrics.Forget(req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if !obj.GetDeletionTimestamp().IsZero() {
		r.Metrics.Forget(req.Name)
		return ctrl.Result{}, nil
	}
	raw, _, _ := unstructured.NestedMap(obj.Object, "spec")
	sp, err := ParseSpec(raw)
	prev, _, _ := unstructured.NestedMap(obj.Object, "status")
	now := r.now()
	if err != nil {
		st := BuildStatus(emptyReport(), Spec{}, obj.GetGeneration(), now, fmt.Errorf("invalid spec: %v", err), prev)
		st["conditions"].([]any)[0].(map[string]any)["reason"] = "InvalidSpec"
		if e := r.setStatus(ctx, req.Name, st); e != nil {
			return ctrl.Result{}, e
		}
		r.Recorder.Eventf(obj, corev1.EventTypeWarning, "InvalidSpec", "%s", clip(err.Error(), 200))
		return ctrl.Result{}, nil // nothing to retry until the spec changes
	}
	// Skip when the last scan is fresh and the spec has not changed.
	if last, ok := prev["lastScanTime"].(string); ok {
		if t, e := time.Parse(time.RFC3339, last); e == nil {
			gen, _, _ := unstructured.NestedInt64(prev, "observedGeneration")
			if gen == obj.GetGeneration() && now.Sub(t) < sp.Interval && !now.Before(t) {
				return ctrl.Result{RequeueAfter: sp.Interval - now.Sub(t)}, nil
			}
		}
	}
	timeout := r.Cfg.ScanTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	sctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := r.now()
	ev, scanErr := r.Scanner.Run(sctx, sp)
	var counts Counts
	var st map[string]any
	if scanErr == nil {
		rep, evalErr := scan.Evaluate(ev, sp.Target, sp.WorkloadNamespace)
		if evalErr != nil {
			scanErr = evalErr
		} else {
			counts = Count(rep)
			st = BuildStatus(rep, sp, obj.GetGeneration(), now, nil, prev)
		}
	}
	if scanErr != nil {
		st = BuildStatus(emptyReport(), sp, obj.GetGeneration(), now, scanErr, prev)
		r.Metrics.Failures.WithLabelValues(req.Name).Inc()
	} else {
		r.Metrics.Observe(req.Name, counts, float64(now.Unix()), r.now().Sub(start).Seconds())
	}
	if err := r.setStatus(ctx, req.Name, st); err != nil {
		return ctrl.Result{}, err
	}
	if scanErr != nil {
		r.Recorder.Eventf(obj, corev1.EventTypeWarning, "ScanFailed", "%s", clip(scanErr.Error(), 200))
		retryAfter := r.Cfg.RetryInterval
		if retryAfter <= 0 {
			retryAfter = 5 * time.Minute
		}
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}
	r.emitTransitions(obj, prev, counts)
	return ctrl.Result{RequeueAfter: sp.Interval}, nil
}

// emitTransitions raises an Event only when something changed since the last status, so a
// steady state does not spam the event stream.
func (r *Reconciler) emitTransitions(obj *unstructured.Unstructured, prev map[string]any, c Counts) {
	pv1, _, _ := unstructured.NestedInt64(prev, "nodes", "cgroupV1")
	ph, _, _ := unstructured.NestedInt64(prev, "nodes", "hybrid")
	first := prev["lastScanTime"] == nil
	switch {
	case c.V1+c.Hybrid > 0 && (first || int64(c.V1) != pv1 || int64(c.Hybrid) != ph):
		r.Recorder.Eventf(obj, corev1.EventTypeWarning, "NodesOnCgroupV1", "%d node(s) on cgroup v1 and %d hybrid of %d Linux nodes", c.V1, c.Hybrid, c.Total)
	case c.V1+c.Hybrid == 0 && !first && (pv1+ph) > 0:
		r.Recorder.Eventf(obj, corev1.EventTypeNormal, "NoNodesOnCgroupV1", "no measured cgroup v1 or hybrid nodes remain; %d node(s) have an unknown mode", c.Unknown)
	case first:
		r.Recorder.Eventf(obj, corev1.EventTypeNormal, "ScanCompleted", "first scan: %d Linux nodes, %d unknown mode, %d high findings", c.Total, c.Unknown, c.High)
	}
}
