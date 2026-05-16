package cmd

import (
	"fmt"

	"github.com/containernetworking/cni/pkg/skel"
)

func Status(args *skel.CmdArgs) (err error) {

	conf, err := LoadNetConf(args.StdinData)
	if err != nil {
		return fmt.Errorf("unable to parse CNI STATUS configuration %s: %w", string(args.StdinData), err)
	}

	log, err := newLogger(conf, args, "STATUS")
	if err != nil {
		return fmt.Errorf("unable to setup logging: %w", err)
	}

	log.Debug("processing CNI STATUS request", "netConf", conf)

	log.Debug("done")
	return nil
}
