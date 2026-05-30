package controller

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	gopherv1alpha1 "github.com/bartlettc22/gopher-cni/api/v1alpha1"
	gcnicni "github.com/bartlettc22/gopher-cni/internal/cni"
)

// reconcileInternalWGSecret ensures the proxy's internal WireGuard Secret exists.
// It stores:
//   - wg.conf  — Interface-only WireGuard config consumed by the CNI plugin
//   - publicKey — public key used by the controller when generating peer client configs
//
// If the Secret is absent a new key pair is generated. Returns the public key string.
func reconcileInternalWGSecret(ctx context.Context, c client.Client, proxy *gopherv1alpha1.GopherProxy) (string, error) {
	secretName := internalWGSecretName(proxy.Name)
	secret := &corev1.Secret{}
	err := c.Get(ctx, types.NamespacedName{Namespace: proxy.Namespace, Name: secretName}, secret)
	if err == nil {
		return string(secret.Data[gcnicni.SecretKeyPublicKey]), nil
	}
	if !apierrors.IsNotFound(err) {
		return "", fmt.Errorf("getting internal WG secret: %w", err)
	}

	privateKey, err := wgtypes.GenerateKey()
	if err != nil {
		return "", fmt.Errorf("generating WireGuard private key: %w", err)
	}
	publicKey := privateKey.PublicKey()

	listenPort := proxy.Spec.InternalListenPort
	if listenPort == 0 {
		listenPort = 51820
	}

	wgConf := buildInternalWGConf(privateKey.String(), proxy.Spec.InternalAddress, int(listenPort))

	secret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: proxy.Namespace,
		},
		Data: map[string][]byte{
			gcnicni.SecretKeyWGConf:    []byte(wgConf),
			gcnicni.SecretKeyPublicKey: []byte(publicKey.String()),
		},
	}
	if err := controllerutil.SetControllerReference(proxy, secret, c.Scheme()); err != nil {
		return "", fmt.Errorf("setting owner reference on internal WG secret: %w", err)
	}
	if err := c.Create(ctx, secret); err != nil {
		return "", fmt.Errorf("creating internal WG secret: %w", err)
	}
	return publicKey.String(), nil
}

// buildInternalWGConf renders a wg.conf with only an [Interface] section.
// The CNI plugin merges this with the peers secret at pod-creation time.
func buildInternalWGConf(privateKey, address string, listenPort int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "[Interface]\n")
	fmt.Fprintf(&sb, "PrivateKey = %s\n", privateKey)
	fmt.Fprintf(&sb, "Address = %s\n", address)
	fmt.Fprintf(&sb, "ListenPort = %d\n", listenPort)
	return sb.String()
}

// reconcilePeersSecret ensures the proxy's peers Secret exists with a peers.conf key.
// The peers.conf starts empty and is populated by reconcilePeers.
func reconcilePeersSecret(ctx context.Context, c client.Client, proxy *gopherv1alpha1.GopherProxy) error {
	secretName := peersSecretName(proxy.Name)
	existing := &corev1.Secret{}
	err := c.Get(ctx, types.NamespacedName{Namespace: proxy.Namespace, Name: secretName}, existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("getting peers secret: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: proxy.Namespace,
		},
		Data: map[string][]byte{
			gcnicni.SecretKeyPeersConf: {},
		},
	}
	if err := controllerutil.SetControllerReference(proxy, secret, c.Scheme()); err != nil {
		return fmt.Errorf("setting owner reference on peers secret: %w", err)
	}
	return c.Create(ctx, secret)
}
