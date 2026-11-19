// sep_order_compare_test.go 对比分隔符顺序对切分效果的影响：
// \n 在第 2 位（默认） vs 第 8 位，其余分隔符保持相对次序。
// 真实文档验证（同 realdoc 约定）：md 缺失时 Skip。
package split

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

const sepOrderDocPath = `D:\go_project\ssai-backend\质量体系策划的管理程序new_37ab4735-0623-42a6-bedf-4ae9edbfef0e.pdf.md`

// orderA 当前默认序：\n 在第 2 位
var orderA = []string{"\n\n", "\n", "。", "！", "！", "？", "？", ";", "；", ".", " "}

// orderB \n 移到第 8 位，其余保持相对次序
var orderB = []string{"\n\n", "。", "！", "！", "？", "？", ";", "\n", "；", ".", " "}

type orderStats struct {
	name    string
	count   int
	minL    int
	maxL    int
	avgL    int
	tailL   int
	endings map[string]int // 切点（保护态块尾分隔符）分布
	buckets map[string]int // 恢复态块长分布
	chunks  []string       // 恢复态块，用于对比首处分歧
}

// chunkEndKey 推断切块实际命中的分隔符：保护态（占位符未恢复）块的尾部字符。
func chunkEndKey(chunk string) string {
	r := []rune(chunk)
	if len(r) == 0 {
		return "空"
	}
	if len(r) >= 2 && r[len(r)-1] == '>' && r[len(r)-2] == '>' {
		return "占位符"
	}
	if len(r) >= 2 && r[len(r)-2] == '\n' && r[len(r)-1] == '\n' {
		return "\\n\\n"
	}
	switch r[len(r)-1] {
	case '\n':
		return "\\n"
	case '。':
		return "。"
	case '！':
		return "！"
	case '？':
		return "？"
	case '；':
		return "；"
	case ';':
		return ";"
	case '.':
		return "."
	case ' ':
		return "空格"
	}
	return "其他:" + string(r[len(r)-1])
}

func measureOrder(t *testing.T, text string, order []string, name string) orderStats {
	s := NewMarkdownPreservingSplitter(384, 284/5)
	s.SetTableSplitThreshold(384)
	s.separators = order

	originalLen := utf8.RuneCountInString(text)
	protected, tableMap, linkMap := s.protectMarkdown(text)
	protChunks := s.splitProtectedText(protected, originalLen, tableMap, linkMap)
	restored := s.restoreMarkdown(protChunks, tableMap, linkMap)

	st := orderStats{name: name, endings: map[string]int{}, buckets: map[string]int{}, chunks: restored}
	minL, maxL, sumL := 1<<30, 0, 0
	for i, c := range restored {
		l := utf8.RuneCountInString(c)
		sumL += l
		if l < minL {
			minL = l
		}
		if l > maxL {
			maxL = l
		}
		if i == len(restored)-1 {
			st.tailL = l
		}
		switch {
		case l <= 192:
			st.buckets["<=192"]++
		case l <= 320:
			st.buckets["193-320"]++
		case l <= 384:
			st.buckets["321-384"]++
		default:
			st.buckets[">384"]++
		}
	}
	st.count = len(restored)
	st.minL, st.avgL, st.maxL = minL, sumL/st.count, maxL
	for _, pc := range protChunks {
		st.endings[chunkEndKey(pc)]++
	}
	return st
}

func sortedEndings(m map[string]int) string {
	type kv struct {
		k string
		v int
	}
	var list []kv
	for k, v := range m {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].v > list[j].v })
	var parts []string
	for _, e := range list {
		parts = append(parts, fmt.Sprintf("%s=%d", e.k, e.v))
	}
	return strings.Join(parts, " ")
}

func printStats(t *testing.T, st orderStats) {
	t.Logf("=== %s ===", st.name)
	t.Logf("块数=%d  块长rune: min=%d avg=%d max=%d  尾块=%d", st.count, st.minL, st.avgL, st.maxL, st.tailL)
	t.Logf("长度分布: <=192:%d 193-320:%d 321-384:%d >384:%d",
		st.buckets["<=192"], st.buckets["193-320"], st.buckets["321-384"], st.buckets[">384"])
	t.Logf("切点分布: %s", sortedEndings(st.endings))
}

func tailStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return string(r)
	}
	return "…" + string(r[len(r)-n:])
}

// writeChunksToFile 将切分结果按「块序号 + 长度 + 内容」写入文本文件。
func writeChunksToFile(t *testing.T, path string, chunks []string) {
	var b strings.Builder
	for i, c := range chunks {
		b.WriteString(fmt.Sprintf("===== chunk %03d | len=%d =====\n%s\n\n", i+1, utf8.RuneCountInString(c), c))
	}
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestSepOrderCompare(t *testing.T) {
	raw, err := os.ReadFile(sepOrderDocPath)
	if err != nil {
		t.Skipf("realdoc md not found: %v", err)
	}
	text := string(raw)
	t.Logf("原文 rune=%d  预计 minChunks=ceil(N/328)=%d", utf8.RuneCountInString(text), (utf8.RuneCountInString(text)+327)/328)

	stA := measureOrder(t, text, orderA, "顺序A：\\n 第2位(默认)")
	stB := measureOrder(t, text, orderB, "顺序B：\\n 第8位")
	printStats(t, stA)
	printStats(t, stB)

	// 两次切分结果落盘（chunk_compare 工件目录）
	writeChunksToFile(t, `D:\go_project\chunk_compare\sep_order_A_newline_pos2.txt`, stA.chunks)
	writeChunksToFile(t, `D:\go_project\chunk_compare\sep_order_B_newline_pos8.txt`, stB.chunks)
	t.Logf("已写入: chunk_compare\\sep_order_A_newline_pos2.txt, chunk_compare\\sep_order_B_newline_pos8.txt")

	// 首处长度的块分歧 + 汇总相同率
	n := stA.count
	if stB.count < n {
		n = stB.count
	}
	same := 0
	for i := 0; i < n; i++ {
		if utf8.RuneCountInString(stA.chunks[i]) == utf8.RuneCountInString(stB.chunks[i]) {
			same++
			continue
		}
		if same == i { // 只记第一处分歧
			t.Logf("首处长度分歧 @块%d: A长=%d 尾=%q | B长=%d 尾=%q",
				i+1, utf8.RuneCountInString(stA.chunks[i]), tailStr(stA.chunks[i], 12),
				utf8.RuneCountInString(stB.chunks[i]), tailStr(stB.chunks[i], 12))
		}
	}
	t.Logf("前 %d 块中长度相同 %d 块", n, same)
}

// TestEffectiveBudget_RealDoc 真实文档回归（有效长度记账）：
// 巨尾块消除；修改记录表裂分碎片不堆积（每块至多 1 个"| 版本 |"表头）。
func TestEffectiveBudget_RealDoc(t *testing.T) {
	raw, err := os.ReadFile(sepOrderDocPath)
	if err != nil {
		t.Skipf("realdoc md not found: %v", err)
	}
	text := string(raw)

	s := NewMarkdownPreservingSplitter(384, 284/5)
	s.SetTableSplitThreshold(384)
	chunks, err := s.SplitText(text)
	if err != nil {
		t.Fatalf("SplitText: %v", err)
	}

	maxLen := 0
	for ci, chunk := range chunks {
		n := utf8.RuneCountInString(chunk)
		if n > maxLen {
			maxLen = n
		}
		if c := strings.Count(chunk, "| 版本 |"); c > 1 {
			t.Fatalf("块 %d 堆积 %d 个修改记录子表碎片:\n%.200s", ci, c, chunk)
		}
	}
	t.Logf("块数=%d maxLen=%d 尾块=%d", len(chunks), maxLen, utf8.RuneCountInString(chunks[len(chunks)-1]))
	// 切分结果落盘（当前构造器分隔符序：\n 第2位去重版）
	writeChunksToFile(t, `D:\go_project\chunk_compare\split_effective_live.txt`, chunks)
	// 最大块只可能是单片超预算碎片（表头 ≈ 530 rune），不得接近两倍预算
	if maxLen >= 2*384 {
		t.Fatalf("最大块 %d rune ≥ 2×预算，疑似碎片堆积或巨尾块", maxLen)
	}
}

// TestSplitText_NoContentLoss_RealDoc 真实文档无损与表格完整性校验：
//  1. 块序列「去重叠前缀后拼接」精确还原保护态文本（无丢内容）
//  2. 块内占位符无截断且全部可恢复（表格/链接原子性）
//  3. 原文每个表格/子表碎片在输出中恰好完整出现一次（不破坏、不重复、不丢失）
//  4. 原文每个链接恰好出现一次
func TestSplitText_NoContentLoss_RealDoc(t *testing.T) {
	raw, err := os.ReadFile(sepOrderDocPath)
	if err != nil {
		t.Skipf("realdoc md not found: %v", err)
	}
	text := string(raw)

	s := NewMarkdownPreservingSplitter(384, 284/5)
	s.SetTableSplitThreshold(384)

	protected, tableMap, linkMap := s.protectMarkdown(text)
	originalLen := utf8.RuneCountInString(text)
	chunks := s.splitProtectedText(protected, originalLen, tableMap, linkMap)
	restored := s.restoreMarkdown(chunks, tableMap, linkMap)

	// 1a. 复刻 splitProtectedText 循环拿到每步的精确重叠（漂移自检见 1b）
	effectiveSize := s.chunkSize - s.chunkOverlap
	if effectiveSize <= 0 {
		effectiveSize = 1
	}
	minChunks := (originalLen + effectiveSize - 1) / effectiveSize
	remainingEff := func(txt string) int {
		eff := s.effectivePrefixes(txt, tableMap, linkMap)
		return eff[len(eff)-1]
	}
	simulated := make([]string, 0, len(chunks))
	overlaps := make([]string, 0, len(chunks))
	remaining := protected
	for remaining != "" && (len(simulated) < minChunks || remainingEff(remaining) > s.chunkSize) {
		chunk, rest := s.findChunk(remaining, tableMap, linkMap)
		if chunk == "" {
			remainingRunes := []rune(remaining)
			eff := s.effectivePrefixes(remaining, tableMap, linkMap)
			pos := locateEffPos(eff, s.chunkSize)
			for _, r := range s.findPlaceholderRanges(remaining) {
				if pos > r[0] && pos < r[1] {
					pos = r[1]
				}
			}
			if pos > len(remainingRunes) {
				pos = len(remainingRunes)
			}
			chunk = string(remainingRunes[:pos])
			rest = string(remainingRunes[pos:])
		}
		simulated = append(simulated, chunk)
		ov := ""
		if s.chunkOverlap > 0 && rest != "" {
			ov = s.semanticOverlap(chunk)
		}
		overlaps = append(overlaps, ov)
		if ov != "" {
			remaining = ov + rest
		} else {
			remaining = rest
		}
	}
	if remaining != "" {
		simulated = append(simulated, remaining)
		overlaps = append(overlaps, "")
	}

	// 1b. 模拟序列必须与实际切分一致（本测试复刻漂移保护）
	if len(simulated) != len(chunks) {
		t.Fatalf("模拟 %d 块 ≠ 实际 %d 块，测试复刻漂移", len(simulated), len(chunks))
	}
	for i := range simulated {
		if simulated[i] != chunks[i] {
			t.Fatalf("块 %d 与模拟序列不一致，测试复刻漂移", i)
		}
	}

	// 1c. 无损还原：每块去掉前一块留下的重叠前缀后拼接 == 保护态文本
	//（overlaps[i] 是块 i 的尾窗，它构成块 i+1 的前缀）
	var rebuilt strings.Builder
	for i, c := range chunks {
		part := c
		if i > 0 && overlaps[i-1] != "" {
			if !strings.HasPrefix(c, overlaps[i-1]) {
				t.Fatalf("块 %d 未以前一块的重叠尾巴开头", i)
			}
			part = strings.TrimPrefix(c, overlaps[i-1])
		}
		rebuilt.WriteString(part)
	}
	if rebuilt.String() != protected {
		t.Fatalf("拼接还原 ≠ 保护态文本（丢内容）: rebuilt=%d protected=%d",
			utf8.RuneCountInString(rebuilt.String()), utf8.RuneCountInString(protected))
	}

	// 2. 占位符无截断且全部可恢复
	phRe := regexp.MustCompile(`<<TABLE_\d+>>|<<LINK_\d+>>`)
	for i, c := range chunks {
		ranges := s.findPlaceholderRanges(c)
		matches := phRe.FindAllString(c, -1)
		if len(ranges) != len(matches) {
			t.Fatalf("块 %d 存在截断占位符: ranges=%d matches=%d", i, len(ranges), len(matches))
		}
		for _, m := range matches {
			if _, ok := tableMap[m]; !ok {
				if _, ok2 := linkMap[m]; !ok2 {
					t.Fatalf("块 %d 占位符 %s 不在映射表", i, m)
				}
			}
		}
	}

	// 3. 表格/子表碎片恰好完整出现一次
	tableRe := regexp.MustCompile(`(\|[^\n]+\|\n\|[-| ]+\|\n(?:\|[^\n]+\|\n?)+)`)
	var frags []string
	for _, m := range tableRe.FindAllString(text, -1) {
		fs := s.splitOversizedTable(m)
		if fs == nil {
			frags = append(frags, m)
		} else {
			frags = append(frags, fs...)
		}
	}
	if len(frags) == 0 {
		t.Fatalf("真实文档应含表格，未识别到")
	}
	for fi, f := range frags {
		cnt := 0
		for _, c := range restored {
			cnt += strings.Count(c, f)
		}
		if cnt != 1 {
			t.Fatalf("表格碎片 %d 出现 %d 次（期望恰好 1 次，破坏/重复/丢失）:\n%.80s", fi, cnt, f)
		}
	}
	t.Logf("表格碎片 %d 个全部恰好出现 1 次", len(frags))

	// 4. 链接恰好出现一次（本文档链接均在正文，无表头复制的多片链接）
	linkRe := regexp.MustCompile(`!?\[([^\]]*)\]\(([^\)]+)\)`)
	for _, m := range linkRe.FindAllString(text, -1) {
		cnt := 0
		for _, c := range restored {
			cnt += strings.Count(c, m)
		}
		if cnt != 1 {
			t.Fatalf("链接 %q 出现 %d 次（期望恰好 1 次）", m, cnt)
		}
	}
}
