package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gopherv1alpha1 "github.com/bartlettc22/gopher-cni/api/v1alpha1"
)

// GopherProxyReconciler reconciles GopherProxy objects.
//
// +kubebuilder:rbac:groups=gopher.cni,resources=gopherproxies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gopher.cni,resources=gopherproxies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;delete
type GopherProxyReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	ProxyImage string
}

// SetupWithManager registers the reconciler and the pod watch that re-triggers
// reconciliation when a pod matching a proxy's peerSelector changes.
func (r *GopherProxyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gopherv1alpha1.GopherProxy{}).
		Owns(&corev1.Pod{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.Service{}).
		// Re-enqueue the owning GopherProxy when any pod in the namespace changes,
		// so peer configs stay in sync as pods come and go.
		Watches(
			&corev1.Pod{},
			handler.EnqueueRequestsFromMapFunc(r.podToProxy),
		).
		Complete(r)
}

// podToProxy maps a pod event to the GopherProxy objects in the same namespace
// whose peerSelector might match this pod.
func (r *GopherProxyReconciler) podToProxy(ctx context.Context, obj client.Object) []reconcile.Request {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return nil
	}

	proxyList := &gopherv1alpha1.GopherProxyList{}
	if err := r.List(ctx, proxyList, client.InNamespace(pod.Namespace)); err != nil {
		return nil
	}

	var reqs []reconcile.Request
	for _, proxy := range proxyList.Items {
		if proxy.Spec.PeerSelector == nil {
			continue
		}
		reqs = append(reqs, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&proxy),
		})
	}
	return reqs
}

func (r *GopherProxyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	proxy := &gopherv1alpha1.GopherProxy{}
	if err := r.Get(ctx, req.NamespacedName, proxy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Phase 1: internal WireGuard key pair
	publicKey, err := reconcileInternalWGSecret(ctx, r.Client, proxy)
	if err != nil {
		return ctrl.Result{}, r.failStatus(ctx, proxy, fmt.Errorf("reconciling internal WG secret: %w", err))
	}

	// Phase 2: peers Secret (created empty; populated by peer reconciliation below)
	if err := reconcilePeersSecret(ctx, r.Client, proxy); err != nil {
		return ctrl.Result{}, r.failStatus(ctx, proxy, fmt.Errorf("reconciling peers secret: %w", err))
	}

	// Phase 3: ClusterIP service (created before the pod so the CNI plugin can resolve it)
	svcName := proxySvcName(proxy.Name)
	if err := reconcileService(ctx, r.Client, proxy); err != nil {
		return ctrl.Result{}, r.failStatus(ctx, proxy, fmt.Errorf("reconciling proxy service: %w", err))
	}

	// Phase 4: peer configs — must run before the pod so the CNI plugin sees a full peer list.
	if err := reconcilePeers(ctx, r.Client, proxy, publicKey, svcName); err != nil {
		logger.Error(err, "reconciling peers")
	}

	// Phase 5: proxy pod — created (or restarted) after peers are written so the CNI
	// plugin picks up the full peer list at pod-creation time.
	peersConf := currentPeersConf(ctx, r.Client, proxy)
	if err := reconcileProxyPod(ctx, r.Client, proxy, r.ProxyImage, peersConf); err != nil {
		return ctrl.Result{}, r.failStatus(ctx, proxy, fmt.Errorf("reconciling proxy pod: %w", err))
	}

	// Derive phase from proxy pod status.
	phase, err := r.proxyPhase(ctx, proxy)
	if err != nil {
		logger.Error(err, "checking proxy pod phase")
	}

	return ctrl.Result{}, r.updateStatus(ctx, proxy, phase, publicKey, svcName)
}

func (r *GopherProxyReconciler) proxyPhase(ctx context.Context, proxy *gopherv1alpha1.GopherProxy) (gopherv1alpha1.ProxyPhase, error) {
	pod := &corev1.Pod{}
	err := r.Get(ctx, client.ObjectKey{Namespace: proxy.Namespace, Name: proxyPodName(proxy.Name)}, pod)
	if apierrors.IsNotFound(err) {
		return gopherv1alpha1.ProxyPhasePending, nil
	}
	if err != nil {
		return gopherv1alpha1.ProxyPhasePending, err
	}
	switch pod.Status.Phase {
	case corev1.PodRunning:
		return gopherv1alpha1.ProxyPhaseRunning, nil
	case corev1.PodFailed:
		return gopherv1alpha1.ProxyPhaseFailed, nil
	default:
		return gopherv1alpha1.ProxyPhasePending, nil
	}
}

func (r *GopherProxyReconciler) updateStatus(ctx context.Context, proxy *gopherv1alpha1.GopherProxy, phase gopherv1alpha1.ProxyPhase, publicKey, svcName string) error {
	patch := client.MergeFrom(proxy.DeepCopy())
	proxy.Status.Phase = phase
	proxy.Status.PodName = proxyPodName(proxy.Name)
	proxy.Status.ServiceName = svcName
	proxy.Status.InternalPublicKey = publicKey
	proxy.Status.PeersSecretName = peersSecretName(proxy.Name)
	return r.Status().Patch(ctx, proxy, patch)
}

func (r *GopherProxyReconciler) failStatus(ctx context.Context, proxy *gopherv1alpha1.GopherProxy, err error) error {
	patch := client.MergeFrom(proxy.DeepCopy())
	proxy.Status.Phase = gopherv1alpha1.ProxyPhaseFailed
	_ = r.Status().Patch(ctx, proxy, patch)
	return err
}
