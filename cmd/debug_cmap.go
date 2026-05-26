//go:build ignore

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	pdfcpuModel "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/font"
)

func main() {
	path := os.Args[1]
	f, _ := os.Open(path)
	defer f.Close()

	conf := pdfcpuModel.NewDefaultConfiguration()
	conf.ValidationMode = pdfcpuModel.ValidationRelaxed
	ctx, err := api.ReadAndValidate(f, conf)
	if err != nil {
		panic(err)
	}

	_, _, inhPAttrs, _ := ctx.PageDict(1, false)
	res := inhPAttrs.Resources
	fontObj, _ := res.Find("Font")
	fontDict := fontObj.(types.Dict)

	for _, name := range []string{"F148", "F157"} {
		entry := fontDict[name]
		indRef := entry.(types.IndirectRef)
		fd, _ := ctx.DereferenceFontDict(indRef)

		toUniRef := fd.IndirectRefEntry("ToUnicode")
		if toUniRef == nil {
			fmt.Printf("Font %s: no ToUnicode\n", name)
			continue
		}

		sd, _, err := ctx.DereferenceStreamDict(*toUniRef)
		if err != nil || sd == nil {
			fmt.Printf("Font %s: ToUnicode dereference failed: %v\n", name, err)
			continue
		}
		sd.Decode()

		content := string(sd.Content)

		// Show the CMap content for ligature-related entries
		fmt.Printf("\n=== Font %s ToUnicode CMap (searching for fi/fl) ===\n", name)
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			if strings.Contains(line, "fi") || strings.Contains(line, "fl") ||
				strings.Contains(line, "FB01") || strings.Contains(line, "fb01") ||
				strings.Contains(line, "FB02") || strings.Contains(line, "fb02") ||
				strings.Contains(line, "0066") || strings.Contains(line, "0069") {
				fmt.Printf("  %s\n", line)
			}
		}

		// Parse and test the CMap
		cmap, err := font.ParseCMap(sd.Content)
		if err != nil {
			fmt.Printf("Font %s: CMap parse error: %v\n", name, err)
			continue
		}

		// Test decode for all possible byte values
		fmt.Printf("\n  Decode test for non-ASCII mappings:\n")
		for b := 0; b < 256; b++ {
			r := cmap.DecodeSingle(b)
			if r > 127 && r != 0 {
				fmt.Printf("    byte 0x%02X → U+%04X (%c)\n", b, r, r)
			}
		}
	}
}
