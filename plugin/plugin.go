package main

import (
	"github.com/bartlettc22/gopher-cni/internal/version"
	"github.com/bartlettc22/gopher-cni/plugin/cmd"
	"github.com/containernetworking/cni/pkg/skel"
	cniVersion "github.com/containernetworking/cni/pkg/version"
)

func main() {
	skel.PluginMainFuncs(skel.CNIFuncs{
		Add:    cmd.Add,
		Del:    cmd.Del,
		Check:  cmd.Check,
		Status: cmd.Status,
	},
		cniVersion.PluginSupports("0.1.0", "0.2.0", "0.3.0", "0.3.1", "0.4.0", "1.0.0", "1.1.0"),
		"Gopher CNI plugin "+version.Version,
	)
}
