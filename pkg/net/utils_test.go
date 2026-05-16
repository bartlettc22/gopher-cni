package net

import (
	"os"
	"strings"
	"testing"

	"github.com/vishvananda/netns"
)

const (
	testNetLinkName = "testlink1"
	testNetNSName   = "testnet1"
	testNetNSPath   = "/var/run/netns/testnet1"
)

func newTestNetNS(t *testing.T) *netns.NsHandle {
	destroyTestNetNSIfExists(t)
	nsHandle, err := netns.NewNamed(testNetNSName)
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	// Ensures our new ns gets cleaned up when the test is done
	t.Cleanup(func() {
		destroyTestNetNSIfExists(t)
	})

	return &nsHandle
}

func destroyTestNetNSIfExists(t *testing.T) {
	err := netns.DeleteNamed(testNetNSName)
	if err != nil {
		if strings.Contains(err.Error(), "no such file or directory") {
			// Namespace didn't exist, no worries
			return
		}

		// Legit problem deleting namespace
		t.Fatalf("got error: %v", err)
	}
}

func skipUnlessRoot(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("Test requires root privileges.")
	}
}
