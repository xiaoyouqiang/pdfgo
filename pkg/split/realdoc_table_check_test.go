package split

// realdoc_table_check_test.go 用真实生产文档验证超长表格预切分效果。
//
// 输入：doc 106（质量体系策划的管理程序.pdf）的生产 md 缓存，含 2 张大表：
//   - §6.1.2 相关方表（约 750 rune）
//   - §9 附录修订记录表（约 1150 rune，含 330 rune 超长单行）
// 预期：两表各裂为多张带表头子表，且无整表超限块（单行超长片段除外）。
//
// 文件不存在时 t.Skip（CI 安全）；报告写出到 REALDOC_OUT（默认
// D:/go_project/chunk_compare/realdoc_table_check.md）。

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRealDocTableSplit(t *testing.T) {
	inPath := os.Getenv("REALDOC_MD")
	if inPath == "" {
		inPath = `D:\go_project\ssai-backend\api\aiservice\data\doc\23sdds232332323232\1\质量体系策划的管理程序_eaa32eba-0283-485b-bfd3-78cf8ae77ce8.pdf.md`
	}
	b, err := os.ReadFile(inPath)
	if err != nil {
		t.Skipf("真实文档不存在，跳过: %v", err)
	}
	markdown := string(b)

	// 复刻 makeTrunkForPdf 管道：Parse(4096) → 清理 → TOC 门控 → 二级切分 384（阈值 0=默认 384）
	model := NewSplitModel(nil, true, true, 4096)
	parents := model.Parse(markdown)
	parents = model.ClearSplitResult(parents)
	tocForm := len(parents) > 1
	children := SplitLargeBlocks(parents, 384, 0, tocForm)

	var sb strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&sb, f+"\n", a...) }

	w("# 真实文档表格预切分验证（父块 4096 / 子块 384 / 表格阈值=默认 384）")
	w("- 输入: %s（%d rune）", inPath, utf8.RuneCountInString(markdown))
	w("- 父块数: %d，子块数: %d", len(parents), len(children))

	over := 0
	maxLen := 0
	for _, c := range children {
		n := utf8.RuneCountInString(c.Content)
		if n > maxLen {
			maxLen = n
		}
		if n > 384 {
			over++
		}
	}
	w("- 子块最大长度: %d rune；超过 384 的块数: %d（应仅为单行超长的修订表片段）", maxLen, over)
	w("")

	w("## 相关方表（§6.1.2）与修订记录表（§9 附录）的裂分结果")
	for pi, p := range parents {
		if !strings.Contains(p.Content, "相关方 Stakeholder") && !strings.Contains(p.Content, "| 版本 |") {
			continue
		}
		w("")
		w("### 父块 %d（标题 %q，%d rune）", pi, strings.TrimSpace(p.Title), utf8.RuneCountInString(p.Content))
		idx := 0
		for _, c := range children {
			if c.Title != p.Title {
				continue
			}
			isTable := strings.Contains(c.Content, "| --- |")
			rows := strings.Count(c.Content, "\n|")
			w("")
			w("#### 子块 %d（%d rune，%d 行表格行，含表头=%v）", idx, utf8.RuneCountInString(c.Content), rows, isTable)
			w("```")
			w("%s", c.Content)
			w("```")
			idx++
		}
	}

	outPath := os.Getenv("REALDOC_OUT")
	if outPath == "" {
		outPath = `D:\go_project\chunk_compare\realdoc_table_check.md`
	}
	if err := os.WriteFile(outPath, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("写报告失败: %v", err)
	}
	t.Logf("报告已写入 %s；子块 %d 个，最大 %d rune，超 384 共 %d 块", outPath, len(children), maxLen, over)
}
