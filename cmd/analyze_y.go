package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

type TB struct {
	idx  int
	Y0   float64
	Y1   float64
	Text string
}

func main() {
	data, _ := os.ReadFile("output/textboxes_raw.txt")

	pageMarker := "======= PAGE 5 TextBoxes"
	idx := strings.Index(string(data), pageMarker)
	if idx < 0 {
		fmt.Println("Page 5 not found")
		return
	}

	pageEnd := "======= PAGE 6 TextBoxes"
	idxEnd := strings.Index(string(data)[idx:], pageEnd)
	content := string(data)[idx : idx+idxEnd]

	var boxes []TB
	lines := strings.Split(content, "\n")
	re := regexp.MustCompile(`TB\[\s*(\d+)\]\s+Y0=\s*([\d.]+)\s+Y1=\s*([\d.]+)`)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if m := re.FindStringSubmatch(line); len(m) == 4 {
			var i int
			var y0, y1 float64
			fmt.Sscanf(m[1], "%d", &i)
			fmt.Sscanf(m[2], "%f", &y0)
			fmt.Sscanf(m[3], "%f", &y1)
			boxes = append(boxes, TB{idx: len(boxes), Y0: y0, Y1: y1, Text: ""})
		} else if strings.HasPrefix(line, "Text:") && len(boxes) > 0 {
			if len(line) > 6 {
				boxes[len(boxes)-1].Text = line[6:]
			}
		} else if len(boxes) > 0 && len(line) > 0 && !strings.HasPrefix(line, "TB[") && !strings.HasPrefix(line, "Text:") && line != "Text:" {
			boxes[len(boxes)-1].Text += " " + line
		}
	}

	fmt.Println("=== Page 5 TextBoxes Y坐标分析 ===")
	fmt.Println()
	for i, tb := range boxes {
		text := tb.Text
		if len(text) > 40 {
			text = text[:40] + "..."
		}
		fmt.Printf("TB[%2d] Y0=%7.1f Y1=%7.1f 高度=%5.1f  Text: %s\n", 
			i, tb.Y0, tb.Y1, tb.Y1-tb.Y0, text)
	}
}
