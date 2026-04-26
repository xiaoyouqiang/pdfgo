// Package main 提供 PDF 文档智能分段命令行工具。
//
// 功能概述：
//   - 读取 PDF 文件，提取文本、表格、图片等内容
//   - 将提取结果转换为 Markdown 格式
//   - 按标题层级结构智能分段，保持上下文完整性
//   - 输出 JSON 格式的分段结果
//
// 使用示例：
//
//	go run cmd/splitter_pdf.go -i input.pdf [-o output.json] [-filter-toc]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract"
	"github.com/xiaoyouqiang/pdfgo/pkg/split"
)

// defaultLimit 是每个分段的最大字符数，默认 4096 个字符
const defaultLimit = 4096

// go run cmd/splitter_pdf.go -i "生生AI规划222.pdf"  -image-dir ./images
func main() {
	var (
		inputFile  string // 输入 PDF 文件路径
		outputFile string // 输出 JSON 文件路径（可选，不指定则输出到标准输出）
		imageDir   string // 图片保存目录（可选，不指定则不保存图片）
		imagePref  string // 图片文件名前缀
		limit      int    // 每个分段的最大字符数
		withFilter bool   // 是否过滤特殊字符
		filterToc  bool   // 是否过滤 PDF 目录条目（点线引导 + 页码格式）
	)

	// 定义命令行参数
	flag.StringVar(&inputFile, "i", "", "输入 PDF 文件路径（必填）")
	flag.StringVar(&outputFile, "o", "", "输出 JSON 文件路径（可选，不指定则输出到控制台）")
	flag.StringVar(&imageDir, "image-dir", "", "图片保存目录（可选，不指定则不保存图片）")
	flag.StringVar(&imagePref, "image-prefix", "img_", "图片文件名前缀")
	flag.IntVar(&limit, "limit", defaultLimit, "每个分段的最大字符数")
	flag.BoolVar(&withFilter, "filter", true, "是否过滤特殊字符")
	flag.BoolVar(&filterToc, "filter-toc", false, "是否过滤 PDF 目录条目（识别 标题...页码 格式的目录行）")
	flag.Parse()

	// 校验必填参数
	if inputFile == "" {
		fmt.Println("Usage: splitter_pdf -i <input.pdf> [-o <output.json>] [-image-dir <dir>] [-image-prefix <prefix>] [-limit 4096] [-filter true]")
		fmt.Println("\nOptions:")
		flag.PrintDefaults()
		os.Exit(1)
	}

	fmt.Printf("Extracting PDF: %s\n", inputFile)

	// 调用核心分段函数
	results, err := SplitPDFDocument(inputFile, imageDir, imagePref, limit, withFilter, filterToc)
	if err != nil {
		log.Fatalf("Failed: %v", err)
	}

	fmt.Printf("Split into %d segments\n", len(results))

	// 将结果序列化为格式化的 JSON
	output, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal results: %v", err)
	}

	// 输出到文件或标准输出
	if outputFile != "" {
		if err := os.WriteFile(outputFile, output, 0644); err != nil {
			log.Fatalf("Failed to write output: %v", err)
		}
		fmt.Printf("Results written to: %s\n", outputFile)
	} else {
		fmt.Println("\n" + string(output))
	}
}

// SplitPDFDocument 提取 PDF 文件内容并智能分段。
//
// 处理流程：
//  1. 创建 PDF 提取器，配置提取选项（文本、表格、图片）
//  2. 提取 PDF 所有页面的内容
//  3. 如指定了图片目录，则保存提取的图片
//  4. 将页面内容转换为 Markdown 格式
//  5. 使用分段模型按标题层级智能分段
//
// 参数：
//   - filePath: PDF 文件路径
//   - imageDir: 图片保存目录，为空则不保存图片
//   - imagePrefix: 图片文件名前缀
//   - limit: 每个分段的最大字符数
//   - withFilter: 是否过滤特殊字符
//   - filterToc: 是否过滤 PDF 目录条目
//
// 返回：
//   - []split.SplitResult: 分段结果数组
//   - error: 处理过程中的错误
func SplitPDFDocument(filePath string, imageDir string, imagePrefix string, limit int, withFilter bool, filterToc bool) ([]split.SplitResult, error) {
	// 创建 PDF 提取器，配置提取选项
	e := pdfextract.NewExtractor(pdfextract.ExtractionOptions{
		ExtractText:  true,           // 始终提取文本
		ExtractTable: true,           // 始终提取表格
		ExtractImage: imageDir != "", // 仅在指定图片目录时提取图片
	})

	// 执行 PDF 提取，返回所有页面的结构化数据
	pages, err := e.ExtractFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to extract PDF: %w", err)
	}

	// 如果指定了图片目录，将提取的图片保存到文件
	if imageDir != "" {
		if err := pdfextract.SaveImages(pages, imageDir, imagePrefix); err != nil {
			return nil, fmt.Errorf("failed to save images: %w", err)
		}
	}

	// 将所有页面转换为 Markdown 格式（包含标题级别检测、表格渲染等）
	markdown := pdfextract.PagesToMarkdown(pages)

	// 创建分段模型并执行分段
	model := split.NewSplitModel(nil, withFilter, filterToc, limit)
	results := model.Parse(markdown)

	return results, nil
}
