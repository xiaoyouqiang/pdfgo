package main

import (
	"fmt"
	"os"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: dump_pdf <file.pdf> [output_dir]")
		os.Exit(1)
	}
	path := os.Args[1]
	outputDir := ""
	if len(os.Args) >= 3 {
		outputDir = os.Args[2]
	}

	e := pdfextract.NewExtractor(pdfextract.ExtractionOptions{
		ExtractText:  true,
		ExtractTable: true,
		ExtractImage: true,
	})
	result, err := e.ExtractFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	pages := result.Pages

	if outputDir != "" {
		if err := pdfextract.SaveImages(pages, outputDir, "img_"); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving images: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Images saved to %s\n\n", outputDir)
	}

	for _, page := range pages {
		fmt.Printf("========== Page %d (%.0f x %.0f) ==========\n", page.PageNum, page.Width, page.Height)

		for _, item := range page.ReadingOrder() {
			if item.Type == "text" {
				fmt.Printf("--- Text ---\n%s\n", item.Text)
			} else if item.Type == "table" {
				tbl := item.Table
				fmt.Printf("--- Table (%dx%d) ---\n", tbl.Rows, tbl.Cols)
				for r := 0; r < tbl.Rows; r++ {
					fmt.Printf("  Row %d:", r)
					for c := 0; c < tbl.Cols; c++ {
						fmt.Printf(" [%s]", tbl.Cells[r][c].Text)
					}
					fmt.Println()
				}
			} else if item.Type == "image" {
				img := item.Image
				saved := img.SavedFilename
				if saved == "" {
					saved = "(not saved)"
				}
				fmt.Printf("--- Image (%dx%d %s) %s ---\n", img.Width, img.Height, img.Format, saved)
			}
		}
		fmt.Println()
	}
}
