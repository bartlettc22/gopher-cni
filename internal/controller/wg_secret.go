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
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	gopherv1alpha1 "github.com/bartlettc22/gopher-cni/api/v1alpha1"
)

const (
	secretKeyPrivateKey = "privateKey"
	secretKeyPublicKey  = "publicKey"
)

// reconcileInternalWGSecret ensures the proxy's internal WireGuard key-pair Secret exists.
// If absent, it generates a new key pair and creates the Secret. Returns the public key.
func reconcileInternalWGSecret(ctx context.Context, c client.Client, proxy *gopherv1alpha1.GopherProxy) (string, error) {
	secretName := internalWGSecretName(proxy.Name)
	secret := &corev1.Secret{}
	err := c.Get(ctx, types.NamespacedName{Namespace: proxy.Namespace, Name: secretName}, secret)
	if err == nil {
		return string(secret.Data[secretKeyPublicKey]), nil
	}
	if !apierrors.IsNotFound(err) {
		return "", fmt.Errorf("getting internal WG secret: %w", err)
	}

	privateKey, err := wgtypes.GenerateKey()
	if err != nil {
		return "", fmt.Errorf("generating WireGuard private key: %w", err)
	}
	publicKey := privateKey.PublicKey()

	secret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: proxy.Namespace,
		},
		Data: map[string][]byte{
			secretKeyPrivateKey: []byte(privateKey.String()),
			secretKeyPublicKey:  []byte(publicKey.String()),
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

// reconcilePeersSecret ensures the proxy's peers Secret exists. The proxy pod mounts
// this Secret and hot-reloads it to update WireGuard peers without restarting.
// Returns true if the Secret was created (i.e., it is empty and needs peer population).
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
		// peers.conf starts empty; the peer reconciler populates it.
		Data: map[string][]byte{
			"peers.conf": {},
		},
	}
	if err := controllerutil.SetControllerReference(proxy, secret, c.Scheme()); err != nil {
		return fmt.Errorf("setting owner reference on peers secret: %w", err)
	}
	return c.Create(ctx, secret)
}
