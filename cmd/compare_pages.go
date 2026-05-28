package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	goRaw, _ := os.ReadFile("output/textboxes_raw.txt")
	pymRaw, _ := os.ReadFile("output/pymupdf_output.txt")

	fmt.Println("=== 21页对比结果 ===")
	for i := 1; i <= 21; i++ {
		// Go
		pageMarkerGo := fmt.Sprintf("======= PAGE %d TextBoxes", i)
		var goBlocks int
		idxGo1 := strings.Index(string(goRaw), pageMarkerGo)
		if idxGo1 >= 0 {
			var contentGo string
			remaining := string(goRaw)[idxGo1:]
			if i < 21 {
				pageMarkerGo2 := fmt.Sprintf("======= PAGE %d TextBoxes", i+1)
				idxGo2 := strings.Index(remaining, pageMarkerGo2)
				if idxGo2 >= 0 {
					contentGo = remaining[:idxGo2]
				}
			} else {
				contentGo = remaining
			}
			goBlocks = strings.Count(contentGo, "TB[")
		}

		// PyMuPDF
		pymPage := fmt.Sprintf("\r\nPAGE %d\r\n", i)
		pymBlocks := 0
		idx1 := strings.Index(string(pymRaw), pymPage)
		if idx1 >= 0 {
			var searchStart int
			if i < 21 {
				pageEnd := fmt.Sprintf("\r\n============================================================\r\nPAGE %d\r\n", i+1)
				searchStart = idx1 + len(pymPage)
				idx2 := strings.Index(string(pymRaw)[searchStart:], pageEnd)
				if idx2 >= 0 {
					pageContent := string(pymRaw)[searchStart:searchStart+idx2]
					pymBlocks = strings.Count(pageContent, "--- Block ")
				}
			} else {
				// page 21: everything after PAGE 21 to end
				searchStart = idx1 + len(pymPage)
				pageContent := string(pymRaw)[searchStart:]
				pymBlocks = strings.Count(pageContent, "--- Block ")
			}
		}

		diff := pymBlocks - goBlocks
		status := "✓"
		if diff > 10 {
			status = "△ PyMuPDF更多"
		} else if diff < -10 {
			status = "▽ Go更多"
		}

		fmt.Printf("Page %2d: Go=%3d PyMuPDF=%3d 差值=%+4d %s\n", i, goBlocks, pymBlocks, diff, status)
	}
}
