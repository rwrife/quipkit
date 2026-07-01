// Package main is the quipkit CLI entrypoint.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/rwrife/quipkit/internal/config"
)

// Version is the quipkit version string. Overridable via -ldflags "-X main.Version=...".
var Version = "0.1.0-dev"

func main() {
	versionFlag := flag.Bool("version", false, "print version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "quipkit %s\n", Version)
		fmt.Fprintln(os.Stderr, "usage: quipkit [--version]")
		fmt.Fprintln(os.Stderr, "  default action: list snippet files in the snippet directory")
		fmt.Fprintln(os.Stderr, "  snippet dir: $QUIPKIT_DIR or ~/.quipkit")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *versionFlag {
		fmt.Printf("quipkit %s\n", Version)
		return
	}

	dir, err := config.SnippetDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "quipkit: %v\n", err)
		os.Exit(1)
	}

	names, err := config.ListSnippetFiles(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "quipkit: %v\n", err)
		os.Exit(1)
	}

	if len(names) == 0 {
		fmt.Fprintf(os.Stderr, "quipkit: no snippets found in %s\n", dir)
		return
	}

	sort.Strings(names)
	for _, n := range names {
		fmt.Println(n)
	}
}
