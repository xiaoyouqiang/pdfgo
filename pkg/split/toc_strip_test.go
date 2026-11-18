package split

// toc_strip_test.go 覆盖目录过滤前置：parseLines 在 smartSplitParagraph
// 之前按行剥除 PDF 风格目录行，保证目录文本长度超过 Parse limit 时，
// 硬切边界不再产生「无页码半行 + 孤页码」残片（doc 106 的 6.6/…8 成因）。

import (
	"fmt"
	"strings"
	"testing"
)

// tocOverLimitDoc 构造目录文本超过 limit 的 markdown：目录标题行 +
// 30 行点线引导目录（单段连排，总长 >1000 rune）+ 正文标题与正文
func tocOverLimitDoc() string {
	var b strings.Builder
	b.WriteString("目 录\n")
	for i := 1; i <= 30; i++ {
		b.WriteString(fmt.Sprintf("%d.%d 系统概述与设计说明章节条目 System Overview Entry %d ........................ %d\n",
			i/10, i%10, i, i))
	}
	b.WriteString("\n## 前言\n这是正文第一段，说明文档的目的与适用范围，不属于目录内容。\n")
	return b.String()
}

// parseTocBlocks helper：Parse + ClearSplitResult
func parseTocBlocks(t *testing.T, md string, limit int) []SplitResult {
	t.Helper()
	model := NewSplitModel(nil, true, true, limit)
	parents := model.Parse(md)
	return model.ClearSplitResult(parents)
}

// assertNoTocRemnant 断言块列表不含点线目录残片与目录条目文本
func assertNoTocRemnant(t *testing.T, blocks []SplitResult) {
	t.Helper()
	joined := ""
	for _, blk := range blocks {
		joined += blk.Content
	}
	if strings.Contains(joined, "....") {
		t.Fatalf("切分结果仍含点线目录残片")
	}
	if strings.Contains(joined, "章节条目") || strings.Contains(joined, "Overview Entry") {
		t.Fatalf("切分结果仍含目录条目文本")
	}
	if strings.Contains(joined, "目 录") {
		t.Fatalf("切分结果仍含独立目录标题行")
	}
}

// TestParseStripsTocOverLimit 目录文本 > limit：切段前剥除，无残片、正文保留
func TestParseStripsTocOverLimit(t *testing.T) {
	blocks := parseTocBlocks(t, tocOverLimitDoc(), 500)
	assertNoTocRemnant(t, blocks)
	joined := ""
	for _, blk := range blocks {
		joined += blk.Content
	}
	if !strings.Contains(joined, "这是正文第一段") {
		t.Fatalf("正文内容丢失")
	}
}

// TestParseStripsTocUnderLimit 目录文本 < limit 的常规路径回归：同样无残片
func TestParseStripsTocUnderLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString("目 录\n")
	for i := 1; i <= 5; i++ {
		b.WriteString(fmt.Sprintf("%d.%d 系统概述章节条目 System Entry %d ............ %d\n", i, i, i, i))
	}
	b.WriteString("\n## 前言\n正文段落内容。\n")
	blocks := parseTocBlocks(t, b.String(), 1500)
	assertNoTocRemnant(t, blocks)
	joined := ""
	for _, blk := range blocks {
		joined += blk.Content
	}
	if !strings.Contains(joined, "正文段落内容") {
		t.Fatalf("正文内容丢失")
	}
}

// TestParseStripsTocMidLineCut 直接复现 doc 106 残片形态：目录文本
// 2235 rune、limit=1500（硬切落在条目行中间）。断言切分块中既无
// 「无页码半行」也无「孤页码」残片。
func TestParseStripsTocMidLineCut(t *testing.T) {
	var b strings.Builder
	b.WriteString("目 录\n")
	// 单行 ~90 rune，行内点线长度随机铺满，25 行 ≈ 2250 rune
	for i := 1; i <= 25; i++ {
		b.WriteString(fmt.Sprintf("%d.%d 质量方针目标的实施与沟通条目 Implementation of Quality Policy Entry %d .............................. %d\n",
			i, i, i, i))
	}
	b.WriteString("\n## 概述\n概述正文。\n")
	blocks := parseTocBlocks(t, b.String(), 1500)
	assertNoTocRemnant(t, blocks)
	// 孤页码残片形如 "...8"：切分块中不应出现「点号+数字」短行
	for i, blk := range blocks {
		for _, line := range strings.Split(blk.Content, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "...") && len(line) <= 6 {
				t.Fatalf("块 %d 孤页码残片: %q", i, line)
			}
		}
	}
}
