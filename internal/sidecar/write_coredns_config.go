package sidecar

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	corefileEnvVar = "COREFILE"
	corefilePath   = "/etc/coredns/Corefile"
)

func RunWriteCoreDNSConfig() {
	corefile := os.Getenv(corefileEnvVar)
	if corefile == "" {
		fmt.Fprintln(os.Stderr, "COREFILE environment variable is not set or empty")
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(corefilePath), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create directory %s: %v\n", filepath.Dir(corefilePath), err)
		os.Exit(1)
	}

	if err := os.WriteFile(corefilePath, []byte(corefile), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write Corefile to %s: %v\n", corefilePath, err)
		os.Exit(1)
	}
}
