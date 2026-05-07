# pdfgo - Pure Go PDF Extraction & Intelligent Splitting

A Go library and CLI tool for extracting content from PDF documents and intelligently splitting text into structured segments. Supports text, tables, and images with heading-level detection and TOC filtering.

## Features

- **Pure Go** - No CGO or external dependencies required for PDF extraction
- **Text Extraction** - Extracts text with font size and position awareness
- **Table Detection** - Detects tables using the algorithm (edges → intersections → cells)
- **Image Extraction** - Saves embedded images to files
- **Heading Level Detection** - Automatically discovers font-size tiers for accurate heading levels
- **Smart Splitting** - Splits content at sentence boundaries while preserving tables and links
- **TOC Filtering** - Optionally filters PDF table-of-contents entries (dot-leader format)
- **Word Document Support** - Also supports `.docx` parsing and splitting
- **Document Title Detection** - Identifies the document title from the first centered text line on page 1, excluding headers/footers
- **Markdown Output** - Converts PDF content to well-structured Markdown

## Installation

```bash
go get github.com/xiaoyouqiang/pdfgo
```

## Quick Start

### CLI - PDF

```bash
go build -o pdfgo ./cmd/splitter_pdf.go

# Basic usage
./pdfgo -i document.pdf

# Save output to file
./pdfgo -i document.pdf -o output.json

# Extract images
./pdfgo -i document.pdf -image-dir ./images -image-prefix "img_"

# Custom segment size and TOC filtering
./pdfgo -i document.pdf -limit 2048 -filter-toc
```

### CLI - Word Document

```bash
go build -o splitter ./cmd/splitter.go

./splitter -i document.docx -o output.json
```

### Library

```go
package main

import (
    "encoding/json"
    "fmt"

    "github.com/xiaoyouqiang/pdfgo/cmd"
    "github.com/xiaoyouqiang/pdfgo/pkg/pdfextract"
    "github.com/xiaoyouqiang/pdfgo/pkg/split"
)

// PDF extraction and splitting
func splitPDF(path string) {
    results, err := cmd.SplitPDFDocument(path, "", "img_", 4096, true, false)
    if err != nil {
        panic(err)
    }
    data, _ := json.MarshalIndent(results, "", "  ")
    fmt.Println(string(data))
}

// Low-level PDF extraction
func extractPDF(path string) {
    e := pdfextract.NewExtractor(pdfextract.ExtractionOptions{
        ExtractText:  true,
        ExtractTable: true,
        ExtractImage: false,
    })
    result, err := e.ExtractFile(path)
    if err != nil {
        panic(err)
    }
    fmt.Println("Title:", result.Title)
    markdown := pdfextract.PagesToMarkdown(result.Pages)
    fmt.Println(markdown)
}

// Markdown splitting only
func splitMarkdown(md string) {
    model := split.NewSplitModel(nil, true, false, 4096)
    results := model.Parse(md)
    data, _ := json.MarshalIndent(results, "", "  ")
    fmt.Println(string(data))
}
```

## CLI Options (splitter_pdf)

| Flag | Default | Description |
|------|---------|-------------|
| `-i` | (required) | Input PDF file path |
| `-o` | (stdout) | Output JSON file path |
| `-image-dir` | (none) | Directory to save extracted images |
| `-image-prefix` | `img_` | Filename prefix for saved images |
| `-limit` | `4096` | Maximum characters per segment |
| `-filter` | `true` | Filter special characters |
| `-filter-toc` | `false` | Filter PDF TOC entries (dot-leader lines) |

## Output Format

```json
[
  {
    "title": "Chapter 1 Overview",
    "content": "This is the content of chapter 1...",
    "keywords": null,
    "parent_chain": ["Chapter 1 Overview"],
    "level": 0
  },
  {
    "title": "1.1 Background",
    "content": "Detailed background information...",
    "keywords": null,
    "parent_chain": ["Chapter 1 Overview", "1.1 Background"],
    "level": 1
  }
]
```

## Architecture

```
pdfgo/
├── cmd/
│   ├── splitter_pdf.go        # PDF CLI entry point
│   ├── splitter.go            # Word document CLI entry point
│   ├── dump_pdf.go            # PDF debugging tool
│   └── test_markdown.go       # Markdown conversion test
├── pkg/
│   ├── pdfextract/            # Core PDF extraction engine
│   │   ├── extractor.go       # Main extractor (PDF → structured pages)
│   │   ├── markdown.go        # Pages → Markdown with heading detection
│   │   ├── model/             # Data structures (Page, TextBox, Char, Rect)
│   │   ├── font/              # Font decoding (Type1, TrueType, Type0/CID)
│   │   ├── interpret/         # PDF content stream interpreter
│   │   ├── layout/            # Layout analysis (chars → lines → text boxes)
│   │   ├── table/             # Table detection (algorithm)
│   │   └── internal/spatial/  # Generic spatial index
│   ├── split/                 # Intelligent text splitting
│   │   ├── splitter.go        # Markdown tree parser & segmenter
│   │   └── text_splitter.go   # Markdown-preserving chunk splitter
│   ├── docx/                  # Word document parser
│   ├── pdf_extractor/         # Python-based PDF extraction (alternative)
│   └── debug/                 # Debug utilities
├── go.mod
└── go.sum
```

### PDF Extraction Pipeline

```
PDF file
  → pdfcpu (parse PDF structure)
  → Content Stream Interpreter (decode operators: Tj, TJ, Tm, Tf, cm, q/Q, re, Do)
  → Font Decoder (Type1/TrueType/Type0 → Unicode)
  → Layout Analyzer (chars → text lines → text boxes)
  → Table Detector (rects/lines → edges → intersections → cells → tables)
  → Markdown Converter (font-size tiers → heading levels, tables → Markdown)
  → Smart Splitter (heading tree → segments, sentence-boundary splitting)
  → JSON output
```

## License

BSL
