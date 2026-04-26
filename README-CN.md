# pdfgo - 纯 Go PDF 内容提取与智能分段

一个 Go 语言的 PDF 文档内容提取和智能分段库，支持文本、表格、图片提取，自动标题层级检测和目录过滤。

## 功能特性

- **纯 Go 实现** - PDF 提取无需 CGO 或外部依赖
- **文本提取** - 基于字体大小和位置信息提取文本
- **表格检测** - 使用 边缘线条检测算法的 算法检测表格（边线 → 交叉点 → 单元格）
- **图片提取** - 保存 PDF 中嵌入的图片到文件
- **标题层级检测** - 自动发现字体大小区间，准确识别标题层级
- **智能分段** - 在句子边界处分段，保护表格和链接不被截断
- **目录过滤** - 可选过滤 PDF 目录条目（点线引导格式）
- **Word 文档支持** - 同时支持 `.docx` 文件的解析和分段
- **Markdown 输出** - 将 PDF 内容转换为结构化 Markdown

## 安装

```bash
go get github.com/xiaoyouqiang/pdfgo
```

## 快速开始

### 命令行 - PDF

```bash
go build -o pdfgo ./cmd/splitter_pdf.go

# 基本用法
./pdfgo -i document.pdf

# 保存输出到文件
./pdfgo -i document.pdf -o output.json

# 提取图片
./pdfgo -i document.pdf -image-dir ./images -image-prefix "img_"

# 自定义分段大小和目录过滤
./pdfgo -i document.pdf -limit 2048 -filter-toc
```

### 命令行 - Word 文档

```bash
go build -o splitter ./cmd/splitter.go

./splitter -i document.docx -o output.json
```

### 作为库使用

```go
package main

import (
    "encoding/json"
    "fmt"

    "github.com/xiaoyouqiang/pdfgo/cmd"
    "github.com/xiaoyouqiang/pdfgo/pkg/pdfextract"
    "github.com/xiaoyouqiang/pdfgo/pkg/split"
)

// PDF 提取和分段
func splitPDF(path string) {
    results, err := cmd.SplitPDFDocument(path, "", "img_", 4096, true, false)
    if err != nil {
        panic(err)
    }
    data, _ := json.MarshalIndent(results, "", "  ")
    fmt.Println(string(data))
}

// 底层 PDF 提取
func extractPDF(path string) {
    e := pdfextract.NewExtractor(pdfextract.ExtractionOptions{
        ExtractText:  true,
        ExtractTable: true,
        ExtractImage: false,
    })
    pages, err := e.ExtractFile(path)
    if err != nil {
        panic(err)
    }
    markdown := pdfextract.PagesToMarkdown(pages)
    fmt.Println(markdown)
}

// 仅 Markdown 分段
func splitMarkdown(md string) {
    model := split.NewSplitModel(nil, true, false, 4096)
    results := model.Parse(md)
    data, _ := json.MarshalIndent(results, "", "  ")
    fmt.Println(string(data))
}
```

## 命令行参数（splitter_pdf）

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-i` | （必填） | 输入 PDF 文件路径 |
| `-o` | （标准输出） | 输出 JSON 文件路径 |
| `-image-dir` | （无） | 图片保存目录 |
| `-image-prefix` | `img_` | 图片文件名前缀 |
| `-limit` | `4096` | 每个分段最大字符数 |
| `-filter` | `true` | 是否过滤特殊字符 |
| `-filter-toc` | `false` | 是否过滤 PDF 目录条目（点线引导行） |

## 输出格式

```json
[
  {
    "title": "第一章 概述",
    "content": "这是第一章的内容...",
    "keywords": null,
    "parent_chain": ["第一章 概述"],
    "level": 0
  },
  {
    "title": "1.1 背景",
    "content": "详细的背景信息...",
    "keywords": null,
    "parent_chain": ["第一章 概述", "1.1 背景"],
    "level": 1
  }
]
```

## 项目结构

```
pdfgo/
├── cmd/
│   ├── splitter_pdf.go        # PDF 命令行入口
│   ├── splitter.go            # Word 文档命令行入口
│   ├── dump_pdf.go            # PDF 调试工具
│   └── test_markdown.go       # Markdown 转换测试
├── pkg/
│   ├── pdfextract/            # 核心 PDF 提取引擎
│   │   ├── extractor.go       # 主提取器（PDF → 结构化页面）
│   │   ├── markdown.go        # 页面 → Markdown（含标题检测）
│   │   ├── model/             # 数据结构（Page, TextBox, Char, Rect）
│   │   ├── font/              # 字体解码（Type1, TrueType, Type0/CID）
│   │   ├── interpret/         # PDF 内容流解释器
│   │   ├── layout/            # 版面分析（字符 → 文本行 → 文本框）
│   │   ├── table/             # 表格检测（边缘线条检测算法的 算法）
│   │   └── internal/spatial/  # 通用空间索引
│   ├── split/                 # 智能文本分段
│   │   ├── splitter.go        # Markdown 树解析与分段
│   │   └── text_splitter.go   # 保护 Markdown 结构的分块器
│   ├── docx/                  # Word 文档解析
│   ├── pdf_extractor/         # 基于 Python 的 PDF 提取（备选方案）
│   └── debug/                 # 调试工具
├── go.mod
└── go.sum
```

### PDF 提取流程

```
PDF 文件
  → pdfcpu（解析 PDF 结构）
  → 内容流解释器（解码操作符：Tj, TJ, Tm, Tf, cm, q/Q, re, Do）
  → 字体解码器（Type1/TrueType/Type0 → Unicode）
  → 版面分析器（字符 → 文本行 → 文本框）
  → 表格检测器（矩形/线条 → 边 → 交叉点 → 单元格 → 表格）
  → Markdown 转换器（字体大小区间 → 标题层级，表格 → Markdown）
  → 智能分段器（标题树 → 分段结果，句子边界切分）
  → JSON 输出
```

## 许可证

BSL
