package controller

import (
	"context"
	"crypto/sha256"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	gopherv1alpha1 "github.com/bartlettc22/gopher-cni/api/v1alpha1"
	gcnicni "github.com/bartlettc22/gopher-cni/internal/cni"
)

const (
	labelProxyName       = "gopher.cni/proxy-name"
	labelComponent       = "app.kubernetes.io/component"
	annotationPeersHash  = "gopher.cni/peers-hash"
)

// reconcileProxyPod ensures the proxy pod exists and is up to date with the current peer list.
// If the peers hash has changed since the pod was created, the pod is deleted so the CNI
// plugin recreates it with the fresh peer list on next scheduling.
func reconcileProxyPod(ctx context.Context, c client.Client, proxy *gopherv1alpha1.GopherProxy, image string, peersConf []byte) error {
	currentHash := peersHash(peersConf)
	podName := proxyPodName(proxy.Name)

	existing := &corev1.Pod{}
	err := c.Get(ctx, types.NamespacedName{Namespace: proxy.Namespace, Name: podName}, existing)
	if err == nil {
		// Pod exists — restart it if the peer list has changed.
		if existing.Annotations[annotationPeersHash] != currentHash {
			if delErr := c.Delete(ctx, existing); delErr != nil && !apierrors.IsNotFound(delErr) {
				return fmt.Errorf("deleting stale proxy pod: %w", delErr)
			}
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("getting proxy pod: %w", err)
	}

	proxyImage := proxy.Spec.Image
	if proxyImage == "" {
		proxyImage = image
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: proxy.Namespace,
			Labels: map[string]string{
				labelProxyName: proxy.Name,
				labelComponent: "gopher-proxy",
				// gopher.cni/enabled is a label, not an annotation, so the webhook picks it up.
				gcnicni.LabelEnabled: "true",
			},
			Annotations: map[string]string{
				// CNI plugin annotations — drive proxy-mode network setup.
				gcnicni.AnnotationCNIMode:                   gcnicni.CNIModeProxy,
				gcnicni.AnnotationWGConfSecret:              proxy.Spec.VPNWGSecret,
				gcnicni.AnnotationProxyInternalWGConfSecret: internalWGSecretName(proxy.Name),
				gcnicni.AnnotationProxyPeersSecret:          peersSecretName(proxy.Name),
				// Tracks the peer list version so we know when to restart.
				annotationPeersHash: currentHash,
			},
		},
		Spec: corev1.PodSpec{
			// No NET_ADMIN, no privileged flag, no sysctls — the CNI plugin (running as
			// root on the node) handles all WireGuard and iptables setup.
			Containers: []corev1.Container{
				{
					Name:  "proxy",
					Image: proxyImage,
					// The proxy binary is a minimal health server; all networking is
					// set up by the CNI plugin at pod-creation time.
					Ports: []corev1.ContainerPort{
						{Name: "health", ContainerPort: 8080, Protocol: corev1.ProtocolTCP},
					},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/healthz",
								Port: intstr.FromInt32(8080),
							},
						},
					},
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(proxy, pod, c.Scheme()); err != nil {
		return fmt.Errorf("setting owner reference on proxy pod: %w", err)
	}
	return c.Create(ctx, pod)
}

// peersHash returns a short hex digest of the peers config content, used to
// detect when the pod needs to be restarted to pick up peer changes.
func peersHash(peersConf []byte) string {
	sum := sha256.Sum256(peersConf)
	return fmt.Sprintf("%x", sum[:8])
}

