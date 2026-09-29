package main

import (
	"os"

	"omakiten/internal/cli"
	"omakiten/internal/terminal"
)

var version = "dev"

func main() {
	os.Exit(cli.Execute(cli.NewRootCommand(version, terminal.Run)))
}
