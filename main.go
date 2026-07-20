package main

import (
	"os"

	"github.com/gustmrg/timesheet-cli/internal/cli"
)

var version = "dev"

func main() {
	os.Exit(cli.Execute(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, version))
}
