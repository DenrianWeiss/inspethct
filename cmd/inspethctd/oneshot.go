package main

import oneshotcmd "inspethct/cmd/inspethctd/oneshot"

func runOneShot(args []string) error {
	return oneshotcmd.Run(args)
}
