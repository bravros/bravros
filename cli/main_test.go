package main

import (
	"testing"

	"github.com/bravros/private/cmd"
)

func TestVersionIsSet(t *testing.T) {
	if cmd.Version == "" {
		t.Fatal("Version must not be empty")
	}
}

func TestExecuteWithHelp(t *testing.T) {
	// Execute with no args shows help and returns nil
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
}
