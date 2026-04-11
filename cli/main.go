package main

import (
	"os"

	"github.com/bravros/private/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
