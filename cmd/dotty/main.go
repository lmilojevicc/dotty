package main

import (
	"fmt"
	"os"

	"github.com/lmilojevicc/dotty/internal/cli"
)

var version = "dev"

func main() {
	cli.Version = version
	cmd := cli.NewRootCommand(os.Stdout, os.Stderr)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
