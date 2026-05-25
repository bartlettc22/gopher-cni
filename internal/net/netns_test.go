package net

import (
	"strings"
	"testing"
)

func TestNetNSHandles(t *testing.T) {
	skipUnlessRoot(t)
	newnshandle := newTestNetNS(t)
	nshandle, netlinkhandle, err := NetNSHandles(testNetNSPath)
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	if !nshandle.Equal(*newnshandle) {
		t.Fatalf("expected %v, got: %v", newnshandle, nshandle)
	}

	// This test could be better to confirm we're in the right netns
	if netlinkhandle == nil {
		t.Fatalf("expected netlinkhandle, got: nil")
	}
}

func TestNetNSHandlesNoExist(t *testing.T) {
	skipUnlessRoot(t)
	_, _, err := NetNSHandles("/tmp/bogusnspath")
	if err != nil {
		if !strings.Contains(err.Error(), "namespace does not exist") {
			t.Fatalf(`expected substring "namespace does not exist", got: %v`, err)
		}
	} else {
		t.Fatalf("expected a failed response for missing namespace, got: no error")
	}
}
