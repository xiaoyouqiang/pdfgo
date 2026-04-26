package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: test_markdown <file.pdf>")
		os.Exit(1)
	}
	path := os.Args[1]

	e := pdfextract.NewExtractor(pdfextract.ExtractionOptions{
		ExtractText:  true,
		ExtractTable: true,
		ExtractImage: false,
	})
	pages, err := e.ExtractFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	md := pdfextract.PagesToMarkdown(pages)

	// Print first 3000 chars
	if len(md) > 3000 {
		fmt.Print(md[:3000])
		fmt.Printf("\n\n... [total %d chars]\n", len(md))
	} else {
		fmt.Print(md)
	}

	// Stats
	fmt.Printf("\n--- Stats ---\n")
	fmt.Printf("Pages: %d\n", len(pages))
	fmt.Printf("Markdown length: %d chars\n", len(md))
	lines := strings.Split(md, "\n")
	h2 := 0
	h3 := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "## ") && !strings.HasPrefix(l, "### ") {
			h2++
		} else if strings.HasPrefix(l, "### ") {
			h3++
		}
	}
	fmt.Printf("H2 headings: %d\n", h2)
	fmt.Printf("H3 headings: %d\n", h3)
}
