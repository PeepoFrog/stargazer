package main

import (
	"github.com/PeepFrog/datastsciparser/internal/cli"
	"github.com/PeepFrog/datastsciparser/internal/runflow"
)

func main() {
	opts := cli.MustParseOptions()
	runflow.MustRun(opts)
}
