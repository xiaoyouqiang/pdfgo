package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	goData, _ := os.ReadFile("output/textboxes_raw.txt")
	pymData, _ := os.ReadFile("output/pymupdf_output.txt")

	// PyMuPDF Page 5
	pymPage5 := strings.Index(string(pymData), "\r\nPAGE 5\r\n")
	pymEnd := strings.Index(string(pymData)[pymPage5:], "\r\n============================================================\r\nPAGE 6\r\n")
	pymContent := string(pymData)[pymPage5 : pymPage5+pymEnd]

	blockStart := strings.Index(pymContent, "--- Block 0 ---")
	if blockStart >= 0 {
		blockContent := pymContent[blockStart:]
		lines := strings.Split(blockContent, "\r\n")
		for i := 0; i < 8 && i < len(lines); i++ {
			fmt.Println(lines[i])
		}
	}

	fmt.Println()
	fmt.Println("=== Go Page 5 第一个TextBox ===")
	goPage5 := strings.Index(string(goData), "======= PAGE 5 TextBoxes")
	goEnd := strings.Index(string(goData)[goPage5:], "======= PAGE 6 TextBoxes")
	goContent := string(goData)[goPage5 : goPage5+goEnd]
	goLines := strings.Split(goContent, "\n")
	for i := 1; i < 7 && i < len(goLines); i++ {
		fmt.Println(strings.TrimSpace(goLines[i]))
	}
}
