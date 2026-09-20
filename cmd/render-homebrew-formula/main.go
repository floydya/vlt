package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"vlt/internal/homebrew"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("render-homebrew-formula", flag.ContinueOnError)
	flags.SetOutput(stderr)
	tag := flags.String("tag", "", "stable release tag")
	checksum := flags.String("sha256", "", "source archive SHA-256")
	output := flags.String("output", "", "formula output path")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "render Homebrew formula: unexpected positional arguments")
		return 2
	}
	if *output == "" {
		_, _ = fmt.Fprintln(stderr, "render Homebrew formula: --output is required")
		return 2
	}
	if err := homebrew.Write(*output, *tag, *checksum); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
