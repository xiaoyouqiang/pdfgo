package main

import (
	"fmt"
	"os"
	"strings"
)

func extractPageContent(raw []byte, pageMarker, pageEnd string) string {
	idx1 := strings.Index(string(raw), pageMarker)
	if idx1 < 0 {
		return ""
	}
	idx2 := strings.Index(string(raw)[idx1:], pageEnd)
	if idx2 < 0 {
		return ""
	}
	return string(raw)[idx1 : idx1+idx2]
}

func countBlocks(content string, prefix string) int {
	return strings.Count(content, prefix)
}

func main() {
	goRaw, _ := os.ReadFile("output/textboxes_raw.txt")
	pymRaw, _ := os.ReadFile("output/pymupdf_output.txt")

	problemPages := []int{3, 5, 6, 7, 10, 11, 12, 21}

	for _, pg := range problemPages {
		fmt.Printf("\n========== PAGE %d 分析 ==========\n", pg)

		// Go
		goMarker := fmt.Sprintf("======= PAGE %d TextBoxes", pg)
		var goContent string
		idxGo := strings.Index(string(goRaw), goMarker)
		if idxGo >= 0 {
			if pg < 21 {
				nextMarker := fmt.Sprintf("======= PAGE %d TextBoxes", pg+1)
				idxEnd := strings.Index(string(goRaw)[idxGo:], nextMarker)
				if idxEnd >= 0 {
					goContent = string(goRaw)[idxGo : idxGo+idxEnd]
				}
			} else {
				goContent = string(goRaw)[idxGo:]
			}
		}

		// PyMuPDF
		pymMarker := fmt.Sprintf("\r\nPAGE %d\r\n", pg)
		var pymContent string
		idxPym := strings.Index(string(pymRaw), pymMarker)
		if idxPym >= 0 {
			if pg < 21 {
				pymEnd := fmt.Sprintf("\r\n============================================================\r\nPAGE %d\r\n", pg+1)
				idxPymEnd := strings.Index(string(pymRaw)[idxPym:], pymEnd)
				if idxPymEnd >= 0 {
					pymContent = string(pymRaw)[idxPym : idxPym+idxPymEnd]
				}
			} else {
				pymContent = string(pymRaw)[idxPym:]
			}
		}

		fmt.Printf("Go块数: %d, PyMuPDF块数: %d\n\n", 
			countBlocks(goContent, "TB["), countBlocks(pymContent, "--- Block "))

		// 分析Go的TextBox分布
		lines := strings.Split(goContent, "\n")
		fmt.Println("Go TextBox Y0 分布:")
		for _, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "TB[") {
				fmt.Print("  ", strings.TrimSpace(line), "\n")
			}
		}
	}
}
