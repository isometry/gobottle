package main

import (
	"errors"
	"os"

	"github.com/isometry/gobottle/cmd"
	"github.com/isometry/gobottle/internal/config"
)

func main() {
	if err := cmd.Execute(); err != nil {
		// Exit 2 distinguishes configuration/validation problems from
		// operational failures for CI scripts.
		var multi *config.MultiError
		var single *config.ValidationError
		if errors.As(err, &multi) || errors.As(err, &single) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
