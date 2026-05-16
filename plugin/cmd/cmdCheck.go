package cmd

import (
	"fmt"

	"github.com/containernetworking/cni/pkg/skel"
)

func Check(args *skel.CmdArgs) (err error) {

	conf, err := LoadNetConf(args.StdinData)
	if err != nil {
		return fmt.Errorf("unable to parse CNI CHECK configuration %s: %w", string(args.StdinData), err)
	}

	log, err := newLogger(conf, args, "CHECK")
	if err != nil {
		return fmt.Errorf("unable to setup logging: %w", err)
	}

	log.Debug("processing CNI CHECK request", "netConf", conf)

	log.Debug("done")
	return nil
}
