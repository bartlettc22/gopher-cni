package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	gopherv1alpha1 "github.com/bartlettc22/gopher-cni/api/v1alpha1"
)

const (
	labelProxyName = "gopher.cni/proxy-name"
	labelComponent = "app.kubernetes.io/component"
)

// reconcileProxyPod ensures the proxy pod exists. It is created once and not updated
// (a delete+recreate is required to apply spec changes).
func reconcileProxyPod(ctx context.Context, c client.Client, proxy *gopherv1alpha1.GopherProxy, image string) error {
	pod := &corev1.Pod{}
	err := c.Get(ctx, types.NamespacedName{Namespace: proxy.Namespace, Name: proxyPodName(proxy.Name)}, pod)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("getting proxy pod: %w", err)
	}

	proxyImage := proxy.Spec.Image
	if proxyImage == "" {
		proxyImage = image
	}

	listenPort := proxy.Spec.InternalListenPort
	if listenPort == 0 {
		listenPort = 51820
	}

	trueVal := true
	pod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      proxyPodName(proxy.Name),
			Namespace: proxy.Namespace,
			Labels: map[string]string{
				labelProxyName: proxy.Name,
				labelComponent: "gopher-proxy",
			},
		},
		Spec: corev1.PodSpec{
			// Sysctls require the pod security policy or a privileged context on the node.
			// ip_forward is needed to route between the two WireGuard interfaces.
			SecurityContext: &corev1.PodSecurityContext{
				Sysctls: []corev1.Sysctl{
					{Name: "net.ipv4.ip_forward", Value: "1"},
				},
			},
			Containers: []corev1.Container{
				{
					Name:  "proxy",
					Image: proxyImage,
					Env: []corev1.EnvVar{
						{
							Name:  "INTERNAL_LISTEN_PORT",
							Value: fmt.Sprintf("%d", listenPort),
						},
						{
							Name:  "INTERNAL_ADDRESS",
							Value: proxy.Spec.InternalAddress,
						},
					},
					SecurityContext: &corev1.SecurityContext{
						Capabilities: &corev1.Capabilities{
							Add: []corev1.Capability{"NET_ADMIN"},
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "internal-wg",
							MountPath: "/etc/gopher-proxy/internal-wg",
							ReadOnly:  true,
						},
						{
							Name:      "vpn-wg",
							MountPath: "/etc/gopher-proxy/vpn-wg",
							ReadOnly:  true,
						},
						{
							Name:      "peers",
							MountPath: "/etc/gopher-proxy/peers",
							ReadOnly:  true,
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "internal-wg",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: internalWGSecretName(proxy.Name),
						},
					},
				},
				{
					Name: "vpn-wg",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: proxy.Spec.VPNWGSecret,
						},
					},
				},
				{
					Name: "peers",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: peersSecretName(proxy.Name),
							// defaultMode keeps the file readable; the proxy binary watches for inotify events.
							Optional: &trueVal,
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
