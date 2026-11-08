package main

import (
	"fmt"
	"os"

	"github.com/douglasmiranda/godownload/internal/config"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: go run %s <download.yaml>\n", os.Args[0])
		os.Exit(2)
	}

	_, diagnostics, err := config.LoadFile(os.Args[1])
	warnings := 0
	for _, diagnostic := range diagnostics {
		fmt.Fprintln(os.Stderr, diagnostic.String())
		if diagnostic.Severity == config.SeverityWarning {
			warnings++
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration invalid: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("configuration valid (%d warning(s))\n", warnings)
}
