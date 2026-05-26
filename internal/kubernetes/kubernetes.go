package kubernetes

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Client is an interface for interacting with Kubernetes
// This allows for mocking in tests
type Client interface {
	GetPod(ctx context.Context, namespace, name string) (*v1.Pod, error)
	FetchSecretKey(ctx context.Context, namespace, name, key string) ([]byte, error)
	GetServiceClusterIP(ctx context.Context, namespace, name string) (string, error)
}

type KubeClient struct {
	client *kubernetes.Clientset
}

func (k KubeClient) GetPod(ctx context.Context, namespace, name string) (*v1.Pod, error) {
	return k.client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
}

func (k KubeClient) FetchSecretKey(ctx context.Context, namespace, name, key string) ([]byte, error) {
	secret, err := k.client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if val, ok := secret.Data[key]; ok {
		return val, nil
	}
	return nil, fmt.Errorf("kubernetes secret %s/%s does not contain data in key %q", namespace, name, key)
}

func (k KubeClient) GetServiceClusterIP(ctx context.Context, namespace, name string) (string, error) {
	svc, err := k.client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if svc.Spec.ClusterIP == "" || svc.Spec.ClusterIP == "None" {
		return "", fmt.Errorf("service %s/%s has no ClusterIP", namespace, name)
	}
	return svc.Spec.ClusterIP, nil
}

// NewClientFromConfigFile returns a Kubernetes client from the given kubeconfig file
func NewClientFromConfigFile(kubeConfigFile string) (Client, error) {
	kubeconfig, err := clientcmd.BuildConfigFromFlags("", kubeConfigFile)
	if err != nil {
		return nil, fmt.Errorf("failed building kubernetes config with kubeconfig %s: %w", kubeConfigFile, err)
	}

	clientset, err := kubernetes.NewForConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed setting up kubernetes client with kubeconfig: %w", err)
	}

	return &KubeClient{
		client: clientset,
	}, nil
}

// NewInClusterClient returns a Kubernetes client using in-cluster configuration
// This should be used when running inside a Kubernetes pod
func NewInClusterClient() (Client, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return &KubeClient{
		client: clientset,
	}, nil
}
