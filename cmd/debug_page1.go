//go:build ignore

package main

import (
	"fmt"
	"os"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	pdfcpuModel "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
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

	// Dump font info for page 1
	_, _, inhPAttrs, _ := ctx.PageDict(1, false)
	
	fmt.Println("=== Page 1 Font Resources ===")
	res := inhPAttrs.Resources
	if res != nil {
		if fontObj, found := res.Find("Font"); found {
			if fontDict, ok := fontObj.(types.Dict); ok {
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
					encoding := ""
					if s := fd.NameEntry("Encoding"); s != nil {
						encoding = *s
					} else if s := fd.NameEntry("Encoding"); s != nil {
						// try indirect ref
						if ir, ok := fd.Find("Encoding"); ok {
							if name, ok := ir.(types.Name); ok {
								encoding = string(name)
							}
						}
					}
					hasToUnicode := "no"
					if fd.IndirectRefEntry("ToUnicode") != nil {
						hasToUnicode = "yes"
					}
					fmt.Printf("  Font %q: subtype=%s baseFont=%q encoding=%q ToUnicode=%s\n", name, subtype, baseFont, encoding, hasToUnicode)
				}
			}
		}
	}

	// Get raw content stream for page 1
	pageDict, _, _, _ := ctx.PageDict(1, false)
	contentBytes, err2 := ctx.PageContent(pageDict, 1)
	if err2 != nil {
		fmt.Printf("Error reading content: %v\n", err2)
		return
	}
	limit := 8000
	if len(contentBytes) < limit {
		limit = len(contentBytes)
	}
	fmt.Printf("\n=== Raw Content Stream (first %d of %d bytes) ===\n", limit, len(contentBytes))
	fmt.Printf("%s\n", string(contentBytes[:limit]))
}
