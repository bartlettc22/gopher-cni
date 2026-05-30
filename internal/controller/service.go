package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	gopherv1alpha1 "github.com/bartlettc22/gopher-cni/api/v1alpha1"
)

// reconcileService ensures a ClusterIP Service exists that routes traffic to the proxy
// pod's internal WireGuard UDP port.
func reconcileService(ctx context.Context, c client.Client, proxy *gopherv1alpha1.GopherProxy) error {
	svc := &corev1.Service{}
	err := c.Get(ctx, types.NamespacedName{Namespace: proxy.Namespace, Name: proxySvcName(proxy.Name)}, svc)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("getting proxy service: %w", err)
	}

	listenPort := proxy.Spec.InternalListenPort
	if listenPort == 0 {
		listenPort = 51820
	}

	svc = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      proxySvcName(proxy.Name),
			Namespace: proxy.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				labelProxyName: proxy.Name,
				labelComponent: "gopher-proxy",
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "wg",
					Protocol:   corev1.ProtocolUDP,
					Port:       listenPort,
					TargetPort: intstr.FromInt32(listenPort),
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(proxy, svc, c.Scheme()); err != nil {
		return fmt.Errorf("setting owner reference on proxy service: %w", err)
	}
	return c.Create(ctx, svc)
}
