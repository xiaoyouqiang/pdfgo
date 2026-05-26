//go:build ignore

package main

import (
	"fmt"
	"os"

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
	if res == nil {
		return
	}

	fontObj, found := res.Find("Font")
	if !found {
		return
	}

	fontDict, ok := fontObj.(types.Dict)
	if !ok {
		return
	}

	for name, entry := range fontDict {
		indRef, ok := entry.(types.IndirectRef)
		if !ok {
			continue
		}
		fd, err := ctx.DereferenceFontDict(indRef)
		if err != nil {
			continue
		}

		subtype := ""
		if s := fd.NameEntry("Subtype"); s != nil {
			subtype = *s
		}
		baseFont := ""
		if s := fd.NameEntry("BaseFont"); s != nil {
			baseFont = *s
		}

		// Check for ToUnicode
		hasToUnicode := "no"
		if fd.IndirectRefEntry("ToUnicode") != nil {
			hasToUnicode = "yes"
		}

		// Only show F148 and F157
		if name == "F148" || name == "F157" {
			fmt.Printf("\nFont %q: subtype=%s baseFont=%q ToUnicode=%s\n", name, subtype, baseFont, hasToUnicode)

			// Check Encoding
			if encObj, ok := fd.Find("Encoding"); ok {
				switch enc := encObj.(type) {
				case types.Name:
					fmt.Printf("  Encoding: %s\n", enc)
				case types.Dict:
					fmt.Printf("  Encoding dict: Type=%v\n", enc.NameEntry("Type"))
					if base, ok := enc.Find("BaseEncoding"); ok {
						fmt.Printf("    BaseEncoding: %v\n", base)
					}
					if diffs, ok := enc.Find("Differences"); ok {
						fmt.Printf("    Differences: %v\n", diffs)
					}
				}
			}

			// Check if there's a charprocs for Type3
			if subtype == "Type3" {
				if cp, ok := fd.Find("CharProcs"); ok {
					if d, ok := cp.(types.Dict); ok {
						fmt.Printf("  CharProcs keys: %v\n", d.Keys())
					}
				}
			}
		}
	}

	// Now test the actual font decoder
	fmt.Println("\n--- Font decoder test ---")
	resolver := font.NewFontResolver()
	font.BuildFontDecoders(ctx, 1, resolver)

	for _, fName := range []string{"F148", "F157"} {
		dec, ok := resolver.Resolve(fName)
		if !ok {
			fmt.Printf("Font %s not found\n", fName)
			continue
		}
		fmt.Printf("Font %s: %s\n", fName, dec.FontName())

		// Try decoding some known bytes that produce FFFD
		// From the content stream, we need to find what byte produces the ligature
		testBytes := [][]byte{
			{0x02}, {0x03}, {0x04}, {0x05}, {0x06},
			{0xFB}, {0xFC}, {0xFD}, {0xFE}, {0xFF},
		}
		for _, b := range testBytes {
			runes, widths := dec.Decode(b)
			for i, r := range runes {
				if r == 0xFFFD || r > 127 {
					fmt.Printf("  byte 0x%02X → U+%04X (%q) width=%.2f\n", b[0], r, string(r), widths[i])
				}
			}
		}
	}
}
