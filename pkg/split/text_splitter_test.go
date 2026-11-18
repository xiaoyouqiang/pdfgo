package split

// text_splitter_test.go 覆盖 protectMarkdown 超长表格预切分（每片保留表头）。

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// makeTable 生成 GFM 表格文本（表头 + 分隔行 + 数据行，结尾换行），与
// protectMarkdown 正则匹配形态一致。
func makeTable(header string, rows []string) string {
	lines := append([]string{header, "| --- | --- |"}, rows...)
	return strings.Join(lines, "\n") + "\n"
}

// makeRow 生成约 18 rune 的数据行
func makeRow(n int) string {
	return "| " + strings.Repeat("甲", n) + " | " + strings.Repeat("乙", n) + " |"
}

// TestSplitOversizedTable_DefaultThreshold 默认阈值=chunkSize：超长表按数据行
// 贪心装箱预切，每片以表头+分隔行开头且 ≤ chunkSize；数据行不丢、不乱序。
func TestSplitOversizedTable_DefaultThreshold(t *testing.T) {
	s := NewMarkdownPreservingSplitter(200, 40)
	rows := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		rows = append(rows, makeRow(4))
	}
	table := makeTable("| 名称 | 数值 |", rows) // ≈ 20×18+20 ≈ 380 rune > 200

	frags := s.splitOversizedTable(table)
	if len(frags) < 2 {
		t.Fatalf("期望切出多片，实际 %d 片", len(frags))
	}
	const headerPrefix = "| 名称 | 数值 |\n| --- | --- |\n"
	var gotRows []string
	for i, frag := range frags {
		if !strings.HasPrefix(frag, headerPrefix) {
			t.Fatalf("片 %d 不以表头+分隔行开头: %q", i, frag)
		}
		if n := utf8.RuneCountInString(frag); n > 200 {
			t.Fatalf("片 %d 超过阈值：%d rune", i, n)
		}
		body := strings.TrimPrefix(frag, headerPrefix)
		gotRows = append(gotRows, strings.Split(body, "\n")...)
	}
	if strings.Join(gotRows, "\n") != strings.Join(rows, "\n") {
		t.Fatalf("数据行丢失或乱序:\n got=%v\nwant=%v", gotRows, rows)
	}
}

// TestSplitOversizedTable_CustomThreshold SetTableSplitThreshold 生效：
// 表格 ≤ 阈值不切；> 阈值才切；n ≤ 0 回退默认 chunkSize。
func TestSplitOversizedTable_CustomThreshold(t *testing.T) {
	s := NewMarkdownPreservingSplitter(384, 76)
	rows := make([]string, 0, 6)
	for i := 0; i < 6; i++ {
		rows = append(rows, makeRow(4))
	}
	table := makeTable("| 编号 | 说明 |", rows) // ≈ 130 rune

	// 默认阈值 384：不切
	if frags := s.splitOversizedTable(table); frags != nil {
		t.Fatalf("阈值内表格不应切分，实际 %d 片", len(frags))
	}
	// 自定义阈值 60：应切分
	s.SetTableSplitThreshold(60)
	frags := s.splitOversizedTable(table)
	if len(frags) < 2 {
		t.Fatalf("超过自定义阈值应切分，实际 %d 片", len(frags))
	}
	for i, frag := range frags {
		if n := utf8.RuneCountInString(frag); n > 60 {
			t.Fatalf("片 %d 超过自定义阈值：%d rune", i, n)
		}
	}
	// n ≤ 0 回退默认：不切
	s.SetTableSplitThreshold(0)
	if frags := s.splitOversizedTable(table); frags != nil {
		t.Fatalf("回退默认阈值后不应切分，实际 %d 片", len(frags))
	}
}

// TestSplitOversizedTable_OversizedHeader 表头块自身超预算：不放弃表头，
// 每片仍拼表头，退化为每片一行。
func TestSplitOversizedTable_OversizedHeader(t *testing.T) {
	s := NewMarkdownPreservingSplitter(200, 40)
	longHeader := "| " + strings.Repeat("超长表头列名", 40) + " | " + strings.Repeat("列", 20) + " |"
	rows := []string{makeRow(4), makeRow(4), makeRow(4)}
	table := makeTable(longHeader, rows)

	frags := s.splitOversizedTable(table)
	if len(frags) != len(rows) {
		t.Fatalf("表头超预算时每片应只装一行，期望 %d 片，实际 %d 片", len(rows), len(frags))
	}
	for i, frag := range frags {
		if !strings.HasPrefix(frag, longHeader+"\n| --- | --- |\n") {
			t.Fatalf("片 %d 未保留超长表头", i)
		}
	}
}

// TestSplitOversizedTable_Malformed 畸形表（行数 < 3，无数据行）不切分。
func TestSplitOversizedTable_Malformed(t *testing.T) {
	s := NewMarkdownPreservingSplitter(100, 20)
	// 长度超阈值但只有表头+分隔行两行
	broken := "| " + strings.Repeat("长", 80) + " |\n| --- | --- |"
	if frags := s.splitOversizedTable(broken); frags != nil {
		t.Fatalf("畸形表应保持原样，实际 %v", frags)
	}
}

// TestSplitText_TableIntegrity SplitText 全流程：占位符恢复后表格完整——
// 每个含数据行的块必含表头，数据行全局不丢失。
func TestSplitText_TableIntegrity(t *testing.T) {
	s := NewMarkdownPreservingSplitter(150, 30)
	rows := make([]string, 0, 12)
	for i := 0; i < 12; i++ {
		rows = append(rows, makeRow(4))
	}
	table := makeTable("| 项 | 值 |", rows)
	text := "前言段落。\n第二行内容用于填充上下文。\n" + table +
		"表格之后的结语段落，用于验证占位符恢复后上下文完整。\n"

	chunks, err := s.SplitText(text)
	if err != nil {
		t.Fatalf("SplitText: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("超阈值文本应切出多块，实际 %d 块", len(chunks))
	}
	const headerPrefix = "| 项 | 值 |\n| --- | --- |\n"
	found := make(map[string]int)
	for ci, chunk := range chunks {
		hasRow := false
		for _, row := range rows {
			if strings.Contains(chunk, row) {
				hasRow = true
				found[row]++
			}
		}
		if hasRow && !strings.Contains(chunk, headerPrefix) {
			t.Fatalf("块 %d 含数据行但缺表头:\n%s", ci, chunk)
		}
	}
	for _, row := range rows {
		if found[row] == 0 {
			t.Fatalf("数据行丢失: %q", row)
		}
	}
}
