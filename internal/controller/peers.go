package controller

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	gopherv1alpha1 "github.com/bartlettc22/gopher-cni/api/v1alpha1"
	gcnicni "github.com/bartlettc22/gopher-cni/internal/cni"
)

// peerEntry holds the parsed state of a single peer in the peers Secret.
type peerEntry struct {
	PodName   string
	PublicKey string
	IP        string // allocated /32 from the proxy's internal subnet
}

// reconcilePeers lists all pods matching the proxy's peerSelector, ensures each has a
// client config Secret, and writes the current peer list into the proxy's peers Secret
// so the proxy binary can hot-reload it.
func reconcilePeers(
	ctx context.Context,
	c client.Client,
	proxy *gopherv1alpha1.GopherProxy,
	proxyPublicKey string,
	proxySvcName string,
) error {
	if proxy.Spec.PeerSelector == nil {
		return nil
	}

	selector, err := metav1.LabelSelectorAsSelector(proxy.Spec.PeerSelector)
	if err != nil {
		return fmt.Errorf("parsing peerSelector: %w", err)
	}

	podList := &corev1.PodList{}
	if err := c.List(ctx, podList,
		client.InNamespace(proxy.Namespace),
		client.MatchingLabelsSelector{Selector: selector},
	); err != nil {
		return fmt.Errorf("listing peer pods: %w", err)
	}

	_, subnet, err := net.ParseCIDR(proxy.Spec.InternalAddress)
	if err != nil {
		return fmt.Errorf("parsing internalAddress %q: %w", proxy.Spec.InternalAddress, err)
	}

	// Load existing peer entries from the peers Secret so we can reuse allocated IPs.
	existing, err := loadPeerEntries(ctx, c, proxy)
	if err != nil {
		return err
	}

	// Build a set of live pod names for stale-entry cleanup.
	livePods := make(map[string]bool, len(podList.Items))
	for i := range podList.Items {
		livePods[podList.Items[i].Name] = true
	}

	// Remove stale entries (pods that no longer exist or no longer match).
	fresh := make([]peerEntry, 0, len(existing))
	for _, e := range existing {
		if livePods[e.PodName] {
			fresh = append(fresh, e)
		}
	}

	allowedIPs := defaultPeerAllowedIPs(proxy)

	listenPort := proxy.Spec.InternalListenPort
	if listenPort == 0 {
		listenPort = 51820
	}

	// Ensure every live pod has a peer entry and a client Secret.
	for i := range podList.Items {
		pod := &podList.Items[i]
		if hasPeerEntry(fresh, pod.Name) {
			continue
		}
		ip, err := allocatePeerIP(subnet, fresh)
		if err != nil {
			return fmt.Errorf("allocating IP for pod %s: %w", pod.Name, err)
		}
		privKey, err := wgtypes.GenerateKey()
		if err != nil {
			return fmt.Errorf("generating WireGuard key for pod %s: %w", pod.Name, err)
		}
		pubKey := privKey.PublicKey()

		entry := peerEntry{
			PodName:   pod.Name,
			PublicKey: pubKey.String(),
			IP:        ip,
		}
		fresh = append(fresh, entry)

		if err := ensurePeerClientSecret(ctx, c, proxy, pod, privKey.String(), pubKey.String(), ip, proxyPublicKey, proxySvcName, listenPort, allowedIPs); err != nil {
			return fmt.Errorf("creating client secret for pod %s: %w", pod.Name, err)
		}
	}

	return writePeersSecret(ctx, c, proxy, fresh)
}

func defaultPeerAllowedIPs(proxy *gopherv1alpha1.GopherProxy) []string {
	if len(proxy.Spec.PeerAllowedIPs) > 0 {
		return proxy.Spec.PeerAllowedIPs
	}
	return []string{"0.0.0.0/0"}
}

// ensurePeerClientSecret creates the WireGuard client config Secret for a peer pod
// if it does not already exist. The pod should reference this Secret via the
// gopher.cni/wgconf-secret annotation.
func ensurePeerClientSecret(
	ctx context.Context,
	c client.Client,
	proxy *gopherv1alpha1.GopherProxy,
	pod *corev1.Pod,
	privateKey, publicKey, peerIP, proxyPublicKey, svcName string,
	listenPort int32,
	allowedIPs []string,
) error {
	secretName := peerClientSecretName(proxy.Name, pod.Name)
	existing := &corev1.Secret{}
	err := c.Get(ctx, types.NamespacedName{Namespace: proxy.Namespace, Name: secretName}, existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("checking client secret: %w", err)
	}

	wgConf := buildClientWGConf(privateKey, peerIP, proxyPublicKey, svcName, listenPort, allowedIPs)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: proxy.Namespace,
			Labels: map[string]string{
				labelProxyName: proxy.Name,
			},
			// Surface the secret name so users know which annotation to set on their pods.
			Annotations: map[string]string{
				"gopher.cni/peer-pod":          pod.Name,
				"gopher.cni/peer-wg-public-key": publicKey,
			},
		},
		Data: map[string][]byte{
			gcnicni.SecretKeyWGConf: []byte(wgConf),
		},
	}
	if err := controllerutil.SetControllerReference(proxy, secret, c.Scheme()); err != nil {
		return err
	}
	return c.Create(ctx, secret)
}

// buildClientWGConf renders a WireGuard client config that routes allowedIPs via the proxy.
func buildClientWGConf(privateKey, peerIP, proxyPublicKey, svcName string, listenPort int32, allowedIPs []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "[Interface]\n")
	fmt.Fprintf(&sb, "PrivateKey = %s\n", privateKey)
	fmt.Fprintf(&sb, "Address = %s/32\n", peerIP)
	fmt.Fprintf(&sb, "\n[Peer]\n")
	fmt.Fprintf(&sb, "PublicKey = %s\n", proxyPublicKey)
	fmt.Fprintf(&sb, "Endpoint = %s:%d\n", svcName, listenPort)
	fmt.Fprintf(&sb, "AllowedIPs = %s\n", strings.Join(allowedIPs, ", "))
	fmt.Fprintf(&sb, "PersistentKeepalive = 25\n")
	return sb.String()
}

// writePeersSecret writes the current peer list to the proxy's peers Secret.
// The proxy binary watches this file and calls wg set to hot-reload.
func writePeersSecret(ctx context.Context, c client.Client, proxy *gopherv1alpha1.GopherProxy, peers []peerEntry) error {
	secretName := peersSecretName(proxy.Name)
	existing := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: proxy.Namespace, Name: secretName}, existing); err != nil {
		return fmt.Errorf("getting peers secret for update: %w", err)
	}

	peersConf := renderPeersConf(peers)
	patch := client.MergeFrom(existing.DeepCopy())
	existing.Data = map[string][]byte{
		"peers.conf": []byte(peersConf),
	}
	return c.Patch(ctx, existing, patch)
}

// renderPeersConf renders all peer entries in WireGuard INI format.
func renderPeersConf(peers []peerEntry) string {
	var sb strings.Builder
	for _, p := range peers {
		fmt.Fprintf(&sb, "[Peer]\n")
		fmt.Fprintf(&sb, "# pod=%s\n", p.PodName)
		fmt.Fprintf(&sb, "PublicKey = %s\n", p.PublicKey)
		fmt.Fprintf(&sb, "AllowedIPs = %s/32\n", p.IP)
		fmt.Fprintf(&sb, "\n")
	}
	return sb.String()
}

// loadPeerEntries reads the existing peer list from the peers Secret.
func loadPeerEntries(ctx context.Context, c client.Client, proxy *gopherv1alpha1.GopherProxy) ([]peerEntry, error) {
	secret := &corev1.Secret{}
	err := c.Get(ctx, types.NamespacedName{Namespace: proxy.Namespace, Name: peersSecretName(proxy.Name)}, secret)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting peers secret: %w", err)
	}
	return parsePeersConf(string(secret.Data["peers.conf"])), nil
}

// currentPeersConf reads the raw peers.conf bytes from the proxy's peers Secret.
// Returns nil if the Secret does not yet exist.
func currentPeersConf(ctx context.Context, c client.Client, proxy *gopherv1alpha1.GopherProxy) []byte {
	secret := &corev1.Secret{}
	err := c.Get(ctx, types.NamespacedName{Namespace: proxy.Namespace, Name: peersSecretName(proxy.Name)}, secret)
	if err != nil {
		return nil
	}
	return secret.Data[gcnicni.SecretKeyPeersConf]
}

// parsePeersConf parses the INI-style peers.conf back into peerEntry structs.
func parsePeersConf(conf string) []peerEntry {
	var entries []peerEntry
	var current peerEntry
	inPeer := false

	for _, line := range strings.Split(conf, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "[Peer]":
			if inPeer && current.PublicKey != "" {
				entries = append(entries, current)
			}
			current = peerEntry{}
			inPeer = true
		case strings.HasPrefix(line, "# pod="):
			current.PodName = strings.TrimPrefix(line, "# pod=")
		case strings.HasPrefix(line, "PublicKey = "):
			current.PublicKey = strings.TrimPrefix(line, "PublicKey = ")
		case strings.HasPrefix(line, "AllowedIPs = "):
			ip := strings.TrimPrefix(line, "AllowedIPs = ")
			ip = strings.TrimSuffix(ip, "/32")
			current.IP = ip
		}
	}
	if inPeer && current.PublicKey != "" {
		entries = append(entries, current)
	}
	return entries
}

func hasPeerEntry(entries []peerEntry, podName string) bool {
	for _, e := range entries {
		if e.PodName == podName {
			return true
		}
	}
	return false
}

// allocatePeerIP finds the lowest unused host IP in subnet (skipping the proxy's own .1 address).
func allocatePeerIP(subnet *net.IPNet, existing []peerEntry) (string, error) {
	used := make(map[uint32]bool)
	for _, e := range existing {
		if ip := net.ParseIP(e.IP); ip != nil {
			used[ipToUint32(ip.To4())] = true
		}
	}

	base := ipToUint32(subnet.IP.To4())
	ones, bits := subnet.Mask.Size()
	size := uint32(1) << (bits - ones)

	// Skip network address (base) and proxy address (base+1).
	for i := uint32(2); i < size-1; i++ {
		candidate := base + i
		if !used[candidate] {
			ip := make(net.IP, 4)
			binary.BigEndian.PutUint32(ip, candidate)
			return ip.String(), nil
		}
	}
	return "", fmt.Errorf("subnet %s is exhausted", subnet)
}

func ipToUint32(ip net.IP) uint32 {
	return binary.BigEndian.Uint32(ip)
}

// labelSelectorMatches returns true if the pod's labels satisfy the selector.
// Used to double-check selector logic in tests.
func labelSelectorMatches(selector labels.Selector, podLabels map[string]string) bool {
	return selector.Matches(labels.Set(podLabels))
}

// sortedPeerNames returns sorted pod names from a peer list (useful for deterministic output).
func sortedPeerNames(peers []peerEntry) []string {
	names := make([]string, len(peers))
	for i, p := range peers {
		names[i] = p.PodName
	}
	sort.Strings(names)
	return names
}
