package main

import (
	"os"

	"github.com/bravros/bravros/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
