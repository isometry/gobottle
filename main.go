package main

import (
	"os"

	"github.com/isometry/gobottle/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
