package install

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html/template"
	"os"
	"path/filepath"

	"github.com/bartlettc22/gopher-cni/pkg/utils"
)

const (
	ServiceAccountPath = "/var/run/secrets/kubernetes.io/serviceaccount"
)

const kubeconfigTemplate = `# Kubeconfig file for Gopher CNI plugin.
apiVersion: v1
kind: Config
clusters:
- name: local
  cluster:
    server: {{.KubernetesServiceProtocol}}://{{.KubernetesServiceHost}}:{{.KubernetesServicePort}}
    {{.TLSConfig}}
users:
- name: cni
  user:
    token: "{{.ServiceAccountToken}}"
contexts:
- name: cni-context
  context:
    cluster: local
    user: cni
current-context: cni-context
`

type kubeconfigFields struct {
	KubernetesServiceProtocol string
	KubernetesServiceHost     string
	KubernetesServicePort     string
	ServiceAccountToken       string
	TLSConfig                 string
}

func createKubeconfigFile(cfg *Config, saToken string) (kubeconfigFilepath string, err error) {
	if cfg.K8sServiceHost == "" {
		return "", fmt.Errorf("kubernetes service host not set")
	}

	if cfg.K8sServicePort == "" {
		return "", fmt.Errorf("kubernetes service port not set")
	}

	var tpl *template.Template
	tpl, err = template.New("kubeconfig").Parse(kubeconfigTemplate)
	if err != nil {
		return
	}

	caFile := cfg.K8sCAFile
	if caFile == "" {
		caFile = ServiceAccountPath + "/ca.crt"
	}

	var tlsConfig string
	if cfg.K8sSkipTLSVerify {
		tlsConfig = "insecure-skip-tls-verify: true"
	} else {
		if !utils.FileExists(caFile) {
			return "", fmt.Errorf("ca file does not exist: %s", caFile)
		}
		var caContents []byte
		caContents, err = os.ReadFile(caFile)
		if err != nil {
			return
		}
		caBase64 := base64.StdEncoding.EncodeToString(caContents)
		tlsConfig = "certificate-authority-data: " + caBase64
	}

	fields := kubeconfigFields{
		KubernetesServiceProtocol: cfg.K8sServiceProtocol,
		KubernetesServiceHost:     cfg.K8sServiceHost,
		KubernetesServicePort:     cfg.K8sServicePort,
		ServiceAccountToken:       saToken,
		TLSConfig:                 tlsConfig,
	}

	var kcbb bytes.Buffer
	if err := tpl.Execute(&kcbb, fields); err != nil {
		return "", err
	}

	var kcbbToPrint bytes.Buffer
	fields.ServiceAccountToken = "<redacted>"
	if !cfg.K8sSkipTLSVerify {
		fields.TLSConfig = fmt.Sprintf("certificate-authority-data: <CA cert from %s>", caFile)
	}
	if err := tpl.Execute(&kcbbToPrint, fields); err != nil {
		return "", err
	}

	kubeconfigFilepath = filepath.Join(cfg.MountedHostDir, cfg.CNINetDir, cfg.KubeconfigFilename)
	log.Info("writing kubeconfig file", "path", kubeconfigFilepath, "contents", kcbbToPrint.String())
	if err = os.WriteFile(kubeconfigFilepath, kcbb.Bytes(), os.FileMode(cfg.KubeconfigMode)); err != nil {
		return "", err
	}
	log.Debug("wrote kubeconfig file", "path", kubeconfigFilepath)

	return
}

func readServiceAccountToken() (string, error) {
	saToken := ServiceAccountPath + "/token"
	if !utils.FileExists(saToken) {
		return "", fmt.Errorf("service account token file %s does not exist. Is this not running within a pod?", saToken)
	}

	token, err := os.ReadFile(saToken)
	if err != nil {
		return "", err
	}

	return string(token), nil
}
