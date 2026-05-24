package cmd

import (
	"fmt"
	"log/slog"
	"net"

	"github.com/bartlettc22/gopher-cni/pkg/cni"
	"github.com/bartlettc22/gopher-cni/pkg/kubernetes"
	"github.com/bartlettc22/gopher-cni/pkg/version"
	"github.com/bartlettc22/gopher-cni/pkg/wireguard"
	"github.com/containernetworking/cni/pkg/skel"
	cnitypes "github.com/containernetworking/cni/pkg/types"
	"github.com/vishvananda/netlink"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// Overrideable functions for unit tests
var newKubeClient = kubernetes.NewClientFromConfigFile
var setupViaHost func(string, string, *wgtypes.Config, *netlink.Addr, []*net.IPNet, *slog.Logger) error = setupWGViaHost
var setupViaContainer func(string, string, *wgtypes.Config, *netlink.Addr, []*net.IPNet, *slog.Logger) error = setupWGViaContainer

func Add(args *skel.CmdArgs) (err error) {

	conf, err := LoadNetConf(args.StdinData)
	if err != nil {
		return fmt.Errorf("unable to parse CNI ADD configuration %s: %w", string(args.StdinData), err)
	}

	log, err := newLogger(conf, args, "ADD")
	if err != nil {
		return fmt.Errorf("unable to configure logging: %w", err)
	}

	log.Debug("processing CNI ADD request", "netConf", conf, "args", args)

	if conf.PrevResult == nil {
		return e(log, "CNI configuration error", fmt.Errorf("plugin type %s does not work without a previous result", version.CNI_PLUGIN_TYPE))
	}
	log.Debug("CNI previous result", "previous", conf.PrevResult)

	if conf.Kubernetes == nil || conf.Kubernetes.Kubeconfig == "" {
		return e(log, "CNI Kubernetes config not provided", nil)
	}

	kubeClient, err := newKubeClient(conf.Kubernetes.Kubeconfig)
	if err != nil {
		return e(log, "failed to create Kubernetes client", err)
	}

	k8s, err := newKubeProviderFromCNI(conf.Kubernetes, args, kubeClient, log)
	if err != nil {
		return e(log, "failed to create Kubernetes provider", err)
	}

	// Update logger with namespace and pod name
	log = log.With("namespace", k8s.PodNamespace(), "pod", k8s.PodName())

	rawWgConf, err := k8s.FetchRawWireguardConfig()
	if err != nil {
		return e(log, "failed to fetch wireguard config", err)
	}

	// rawWgConf is nil if the resource was excluded
	if rawWgConf != nil {
		wgConfig, err := wireguard.ParseConfig(rawWgConf)
		if err != nil {
			return e(log, "failed to unmarshal wireguard config", err)
		}

		wgAddr, err := netlink.ParseAddr(wgConfig.Address.String())
		if err != nil {
			return e(log, "failed to parse wireguard address", err)
		}

		splitTunnelCIDRs, err := k8s.SplitTunnelCIDRs()
		if err != nil {
			return e(log, "failed to parse split-tunnel CIDRs", err)
		}

		switch k8s.CNIMode() {
		case cni.CNIModeHostOrigin:
			if err := setupViaHost(args.Netns, cni.InterfaceName, wgConfig.WGConfig, wgAddr, splitTunnelCIDRs, log); err != nil {
				return e(log, "failed to setup network (host-origin)", err)
			}
		case cni.CNIModePodOrigin:
			if err := setupViaContainer(args.Netns, cni.InterfaceName, wgConfig.WGConfig, wgAddr, splitTunnelCIDRs, log); err != nil {
				return e(log, "failed to setup network (pod-origin)", err)
			}
		default:
			return e(log, "unknown CNI mode", fmt.Errorf("unknown CNI mode: %s", k8s.CNIMode()))
		}
	}

	log.Debug("done")
	return cnitypes.PrintResult(conf.PrevResult, conf.CNIVersion)
}
