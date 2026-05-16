package cmd

import (
	"fmt"

	"github.com/containernetworking/cni/pkg/skel"
)

func Del(args *skel.CmdArgs) (err error) {

	conf, err := LoadNetConf(args.StdinData)
	if err != nil {
		return fmt.Errorf("unable to parse CNI DEL configuration %s: %w", string(args.StdinData), err)
	}

	log, err := newLogger(conf, args, "DEL")
	if err != nil {
		return fmt.Errorf("unable to setup logging: %w", err)
	}

	log.Debug("processing CNI DEL request", "netConf", conf)

	log.Debug("done")
	return nil
}
