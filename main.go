// Package main is the entry point for the sonar DNS propagation checker.
// It delegates all flag parsing, orchestration, and output to the cmd package.
package main

import (
	"os"

	"github.com/samuelbailey123/sonar/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
