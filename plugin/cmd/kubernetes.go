package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"slices"

	"github.com/bartlettc22/gopher-cni/pkg/cni"
	"github.com/bartlettc22/gopher-cni/pkg/kubernetes"
	"github.com/containernetworking/cni/pkg/skel"
	cnitypes "github.com/containernetworking/cni/pkg/types"
	v1 "k8s.io/api/core/v1"
)

// These fields come from the invocation of the CNI from Kubernetes
type K8sArgs struct {
	cnitypes.CommonArgs
	IP                         net.IP
	K8S_POD_NAME               cnitypes.UnmarshallableString // nolint: golint, stylecheck
	K8S_POD_NAMESPACE          cnitypes.UnmarshallableString // nolint: golint, stylecheck
	K8S_POD_INFRA_CONTAINER_ID cnitypes.UnmarshallableString // nolint: golint, stylecheck
}

type CNIProvider struct {
	client            kubernetes.Client
	pod               *v1.Pod
	excludeNamespaces []string
	log               *slog.Logger
}

// func newKubeProviderFromCNI(conf *cni.PluginKubernetesConfig, args *skel.CmdArgs) (KubeCNIProvider, error) {
func newKubeProviderFromCNI(conf *cni.PluginKubernetesConfig, args *skel.CmdArgs, client kubernetes.Client, log *slog.Logger) (*CNIProvider, error) {

	var err error

	if args == nil {
		return nil, fmt.Errorf("CNI plugin args not provided")
	}

	if conf == nil {
		return nil, fmt.Errorf("CNI Kubernetes spec not provided")
	}

	k8sArgs := K8sArgs{}
	if err := cnitypes.LoadArgs(args.Args, &k8sArgs); err != nil {
		return nil, err
	}

	podName := string(k8sArgs.K8S_POD_NAME)
	podNamespace := string(k8sArgs.K8S_POD_NAMESPACE)
	p := &CNIProvider{
		client:            client,
		excludeNamespaces: conf.ExcludeNamespaces,
		log:               log,
	}

	if podName == "" || podNamespace == "" {
		return nil, fmt.Errorf("CNI runtime did not provide namespace/pod info; namespace: %q, pod: %q", podNamespace, podName)
	}

	p.pod, err = client.GetPod(context.Background(), podNamespace, podName)
	if err != nil {
		return nil, fmt.Errorf("failed getting pod %s/%s: %w", podNamespace, podName, err)
	}

	return p, nil
}

// FetchRawWireguardConfig fetches the raw wireguard configuration from the labeld secret
// If the label is not set, returns nil and no error
func (p CNIProvider) FetchRawWireguardConfig() ([]byte, error) {
	if !slices.Contains(p.excludeNamespaces, p.pod.GetNamespace()) {
		if enabled := fetchLabel(p.pod, cni.LabelEnabled); enabled == "true" {
			if secretName := fetchAnnotation(p.pod, cni.AnnotationWGConfSecret); secretName != "" {
				return fetchSecretKey(p.client, p.PodNamespace(), secretName, cni.SecretKeyWGConf)
			} else {
				return nil, fmt.Errorf("missing required annotation: %s must specify a secret name", cni.AnnotationWGConfSecret)
			}
		} else {
			p.log.Debug("resource excluded from processing; no '" + cni.LabelEnabled + "' label")
		}
	} else {
		p.log.Debug("resource excluded from processing; namespace excluded")
	}

	return nil, nil
}

func (p CNIProvider) PodName() string {
	return p.pod.GetName()
}

func (p CNIProvider) PodNamespace() string {
	return p.pod.GetNamespace()
}

func (p CNIProvider) CNIMode() string {
	mode := cni.CNIModePodOrigin
	if p.pod.Annotations != nil {
		if val, ok := p.pod.Annotations[cni.AnnotationCNIMode]; ok && val != "" {
			mode = val
		}
	}
	return mode
}

// fetchLabel returns the value of the label if it exists in the pod's labels
func fetchLabel(pod *v1.Pod, label string) string {
	for k, v := range pod.GetLabels() {
		if k == label && v != "" {
			return v
		}
	}
	return ""
}

// fetchAnnotation returns the value of the annotation if it exists in the pod's annotations
func fetchAnnotation(pod *v1.Pod, annotation string) string {
	for k, v := range pod.GetAnnotations() {
		if k == annotation && v != "" {
			return v
		}
	}
	return ""
}

// fetchSecretKey returns the value of the key in the secret if it exists
// Returns an error if the secret or key does not exist
func fetchSecretKey(client kubernetes.Client, namespace, name, key string) ([]byte, error) {
	secret, err := client.GetSecret(context.TODO(), namespace, name)
	if err != nil {
		return nil, err
	}

	if val, ok := secret.Data[key]; ok {
		return val, nil
	}

	return nil, fmt.Errorf("kubernetes secret %s/%s does not contain data in key %q", namespace, name, key)
}
