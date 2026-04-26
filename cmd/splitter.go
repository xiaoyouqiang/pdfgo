package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/xiaoyouqiang/pdfgo/pkg/docx"
	"github.com/xiaoyouqiang/pdfgo/pkg/split"
)

// SplitOptions holds the splitting options
type SplitOptions struct {
	InputFile       string
	OutputFile      string
	ImageDir        string
	ImagePrefix     string
	ImageUniqueName bool
	Limit           int
	WithFilter      bool
}

// DefaultLimit is the default character limit per segment
const DefaultLimit = 4096

func main() {
	opts := SplitOptions{}

	flag.StringVar(&opts.InputFile, "i", "", "Input Word file path (.docx)")
	flag.StringVar(&opts.OutputFile, "o", "", "Output JSON file path (optional)")
	flag.StringVar(&opts.ImageDir, "image-dir", "", "Directory to save images (optional)")
	flag.StringVar(&opts.ImagePrefix, "image-prefix", "", "Prefix for image filenames (optional)")
	flag.BoolVar(&opts.ImageUniqueName, "image-unique", false, "Generate unique image names using UUID")
	flag.IntVar(&opts.Limit, "limit", DefaultLimit, "Maximum characters per segment")
	flag.BoolVar(&opts.WithFilter, "filter", true, "Filter special characters")
	flag.Parse()

	if opts.InputFile == "" {
		fmt.Println("Usage: go-smart-split -i <input.docx> [-o <output.json>] [-image-dir <dir>] [-image-prefix <prefix>] [-image-unique] [-limit 4096] [-filter true]")
		fmt.Println("\nOptions:")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// Read Word document
	fmt.Printf("Reading document: %s\n", opts.InputFile)

	doc, err := docx.ParseWordFile(opts.InputFile)
	if err != nil {
		log.Fatalf("Failed to parse document: %v", err)
	}

	//fmt.Printf("%v", doc.Tables)

	fmt.Printf("Document parsed: %d paragraphs, %d tables, %d images\n", len(doc.Paragraphs), len(doc.Tables), len(doc.Images))

	// Generate markdown and optionally save images
	var markdown string
	if opts.ImageDir != "" {
		// Save images and get markdown with updated paths
		if err := doc.SaveImages(opts.ImageDir, opts.ImagePrefix, opts.ImageUniqueName); err != nil {
			log.Fatalf("Failed to save images: %v", err)
		}
		markdown = doc.GetImageMarkdownWithPrefix(doc.ToMarkdown(), opts.ImagePrefix, opts.ImageUniqueName)
		fmt.Printf("Images saved to: %s\n", opts.ImageDir)
	} else {
		markdown = doc.ToMarkdown()
	}

	//println(markdown)

	fmt.Printf("Generated markdown: %d characters\n", len(markdown))

	// Split markdown using primary splitting
	model := split.NewSplitModel(nil, opts.WithFilter, false, opts.Limit)
	results := model.Parse(markdown)

	fmt.Printf("Split into %d segments (before secondary split)\n", len(results))

	// Apply secondary splitting for large blocks (preserve tables and links)
	secondaryChunkSize := 256 // Smaller chunk size for secondary splitting
	secondaryOverlap := 12    // Overlap for context
	results = split.SplitLargeBlocks(results, secondaryChunkSize, secondaryOverlap)

	fmt.Printf("Split into %d segments (after secondary split)\n", len(results))

	// Output results
	output, marshalErr := json.MarshalIndent(results, "", "  ")
	if marshalErr != nil {
		log.Fatalf("Failed to marshal results: %v", marshalErr)
	}

	if opts.OutputFile != "" {
		if writeErr := os.WriteFile(opts.OutputFile, output, 0644); writeErr != nil {
			log.Fatalf("Failed to write output: %v", writeErr)
		}
		fmt.Printf("Results written to: %s\n", opts.OutputFile)
	} else {
		fmt.Println("\n" + string(output))
	}
}

// SplitDocument is the library interface for splitting Word documents
func SplitDocument(filePath string, limit int, withFilter bool) ([]split.SplitResult, error) {
	return SplitDocumentWithImages(filePath, "", "", false, limit, withFilter)
}

// SplitDocumentWithImages is the library interface for splitting Word documents with image saving
// Parameters:
//   - filePath: Path to the Word document
//   - imageDir: Directory to save images (empty to not save)
//   - imagePrefix: Prefix for image filenames (optional)
//   - useUniqueName: Generate unique image names using UUID
//   - limit: Maximum characters per segment
//   - withFilter: Enable special character filtering
func SplitDocumentWithImages(filePath string, imageDir string, imagePrefix string, useUniqueName bool, limit int, withFilter bool) ([]split.SplitResult, error) {
	doc, err := docx.ParseWordFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse document: %w", err)
	}

	var markdown string
	if imageDir != "" {
		// Save images and get markdown with updated paths
		if err := doc.SaveImages(imageDir, imagePrefix, useUniqueName); err != nil {
			return nil, fmt.Errorf("failed to save images: %w", err)
		}
		markdown = doc.GetImageMarkdownWithPrefix(doc.ToMarkdown(), imagePrefix, useUniqueName)
	} else {
		markdown = doc.ToMarkdown()
	}

	model := split.NewSplitModel(nil, withFilter, false, limit)
	results := model.Parse(markdown)

	return results, nil
}

// SplitMarkdown is the library interface for splitting markdown text
func SplitMarkdown(markdown string, limit int, withFilter bool) []split.SplitResult {
	model := split.NewSplitModel(nil, withFilter, false, limit)
	return model.Parse(markdown)
}
