package split

// text_splitter_test.go 覆盖 protectMarkdown 超长表格预切分（每片保留表头）。

import (
	"fmt"
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

// ===== 有效长度记账（恢复态预算）=====

// TestEffectivePrefixes_TotalEqualsOriginal 有效前缀表的不变量：
// 单调非降（超短链接使占位符恢复后变短的场景也被钳制兜住），
// 有效总长恒等于原文 rune 数。
func TestEffectivePrefixes_TotalEqualsOriginal(t *testing.T) {
	s := NewMarkdownPreservingSplitter(150, 30)
	table := makeTable("| 名称 | 数值 |", []string{makeRow(4), makeRow(4)})
	text := "开头段落。\n" + table + "结尾有链接 [文档](https://example.com/a) 与正文。\n"

	protected, tableMap, linkMap := s.protectMarkdown(text)
	eff := s.effectivePrefixes(protected, tableMap, linkMap)

	for i := 1; i < len(eff); i++ {
		if eff[i] < eff[i-1] {
			t.Fatalf("eff 在 %d 处回退: %d < %d", i, eff[i], eff[i-1])
		}
	}
	if got, want := eff[len(eff)-1], utf8.RuneCountInString(text); got != want {
		t.Fatalf("有效总长 %d ≠ 原文长度 %d", got, want)
	}
}

// TestSplitText_FragmentsNotStacked 裂分碎片不得堆积回同一块：
// 占位符恢复后的完整长度计入块预算，每个恢复后的块至多 1 个子表碎片。
func TestSplitText_FragmentsNotStacked(t *testing.T) {
	s := NewMarkdownPreservingSplitter(150, 30)
	rows := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		rows = append(rows, makeRow(4))
	}
	table := makeTable("| 名称 | 数值 |", rows) // ≈ 344 rune > 150，裂分为多片
	text := "前文。\n" + table + "后文。\n"

	chunks, err := s.SplitText(text)
	if err != nil {
		t.Fatalf("SplitText: %v", err)
	}
	const headerPrefix = "| 名称 | 数值 |\n| --- | --- |\n"
	headerChunks := 0
	for ci, chunk := range chunks {
		if n := strings.Count(chunk, headerPrefix); n > 1 {
			t.Fatalf("块 %d 堆积了 %d 个子表碎片:\n%s", ci, n, chunk)
		}
		if strings.Contains(chunk, headerPrefix) {
			headerChunks++
		}
	}
	if headerChunks < 2 {
		t.Fatalf("裂分碎片应分布在多块，实际仅 %d 块含表头", headerChunks)
	}
}

// TestSplitText_OversizedHeaderFragmentPerChunk 表头块贴近阈值（修改记录表
// 的真实形态）：预算装不下第 2 行数据 → 每片=表头+1 行，恢复后每片独占
// 一块，不与他片同块（单片略超阈值是整表原子性的既定取舍）。
func TestSplitText_OversizedHeaderFragmentPerChunk(t *testing.T) {
	s := NewMarkdownPreservingSplitter(200, 40)
	longHeader := "| " + strings.Repeat("修改章节内容简述中英文混排", 13) + " | 版本 |" // ≈179
	rows := []string{makeRow(4), makeRow(4), makeRow(4), makeRow(4)}
	table := makeTable(longHeader, rows) // 表头块 ≈192 < 200 → 每片预算只剩一行

	chunks, err := s.SplitText(table)
	if err != nil {
		t.Fatalf("SplitText: %v", err)
	}
	frags := 0
	for ci, chunk := range chunks {
		if n := strings.Count(chunk, "| --- | --- |"); n > 1 {
			t.Fatalf("块 %d 含 %d 个子表（碎片堆积）:\n%.200s", ci, n, chunk)
		}
		if strings.Contains(chunk, "| --- | --- |") {
			frags++
		}
	}
	if frags != len(rows) {
		t.Fatalf("期望 %d 个碎片各自成块，实际 %d 块含子表", len(rows), frags)
	}
}

// TestSplitText_NoGiantTail 循环耗尽估算块数后，剩余内容有效长度仍超过
// 一个块预算时必须继续切，尾块不得超出预算（消除巨尾块）。
func TestSplitText_NoGiantTail(t *testing.T) {
	s := NewMarkdownPreservingSplitter(384, 76)
	var b strings.Builder
	// 行长 ≈ 249：切点稳定落在窗口下沿附近，每轮消耗 ≈ 250 < effectiveSize=308，
	// 旧逻辑在此形态下会留下数千 rune 的巨尾块
	line := "| " + strings.Repeat("字", 244) + " |"
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "%s\n", line)
	}
	chunks, err := s.SplitText(b.String())
	if err != nil {
		t.Fatalf("SplitText: %v", err)
	}
	if len(chunks) < 30 {
		t.Fatalf("10000 rune 文本应切出约 33 块，实际 %d 块", len(chunks))
	}
	if n := utf8.RuneCountInString(chunks[len(chunks)-1]); n > 384 {
		t.Fatalf("尾块 %d rune 超过预算 384（巨尾块）", n)
	}
}
