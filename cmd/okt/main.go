package main

import (
	"fmt"
	"os"

	"omakiten/internal/cli"
)

var version = "dev"

func main() {
	if err := cli.NewRootCommand(version).Execute(); err != nil {
		if code, ok := cli.ExitCode(err); ok {
			os.Exit(code)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
