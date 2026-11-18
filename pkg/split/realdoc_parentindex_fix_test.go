package split

// realdoc_parentindex_fix_test.go 临时验证：doc 106 真实文档上 ParentIndex
// 预剥离修复的效果。校验方式：剥掉子块经 MakePathTrunk 前置的路径标题与
// 尾部空格取原始切片，断言其包含于所指父块内容；切分器可能对片段做空白
// 归一化，故另设去空白归一化后的二级判定（仅空白差异≠错位）。
// 报告写出到 REALDOC_OUT（默认 D:/go_project/chunk_compare/realdoc_parentindex_fix.md）。

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestRealDocParentIndexFix(t *testing.T) {
	inPath := os.Getenv("REALDOC_MD")
	if inPath == "" {
		inPath = `D:\go_project\ssai-backend\api\aiservice\data\doc\23sdds232332323232\1\质量体系策划的管理程序_eaa32eba-0283-485b-bfd3-78cf8ae77ce8.pdf.md`
	}
	b, err := os.ReadFile(inPath)
	if err != nil {
		t.Skipf("真实文档不存在，跳过: %v", err)
	}
	markdown := string(b)
	docTitle := "质量体系策划的管理程序"

	var sb strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&sb, f+"\n", a...) }

	model := NewSplitModel(nil, true, true, 1500)
	parents := model.Parse(markdown)
	parents = model.ClearSplitResult(parents)

	// 目录残片检查（前置剥除效果）：Parse+Clear 后不应再含点线引导残片
	// （\.{3,} 同时命中「无页码半行」与「孤页码 ...8」两类残片）
	reDots := regexp.MustCompile(`\.{3,}`)
	tocRemnants := 0
	for _, p := range parents {
		if reDots.MatchString(p.Content) {
			tocRemnants++
		}
	}

	// 旧管道（api 排序，修复前行为）：先切后 MPT
	oldChildren := SplitLargeBlocks(parents, 384, 0, true)
	oldParents := MakePathTrunk(docTitle, parents)
	oldChildren = MakePathTrunk(docTitle, oldChildren)

	// 新管道（预剥离）
	newParents, newChildren := SplitDocumentTrunks(parents, 384, 0, docTitle)

	w("# doc 106 ParentIndex 预剥离修复验证")
	w("- 输入 %d rune，docTitle=%q，Parse 后块数 %d", utf8.RuneCountInString(markdown), docTitle, len(parents))
	w("- 目录残片块（含 3+ 连续点号，Parse+Clear 后）: %d", tocRemnants)

	// 剥掉 MPT 组装层（Content = Title + "\n" + piece + " "）取子块原始切片
	rawPiece := func(c SplitResult) string {
		s := strings.TrimSuffix(c.Content, " ")
		s = strings.TrimPrefix(s, c.Title+"\n")
		return strings.TrimSpace(s)
	}

	// squash 去全部空白 rune：判定"仅空白差异"
	squash := func(s string) string {
		var bd strings.Builder
		bd.Grow(len(s))
		for _, r := range s {
			if !unicode.IsSpace(r) {
				bd.WriteRune(r)
			}
		}
		return bd.String()
	}

	// stripTable 再去掉表格骨架符（| -）：表格保护器会重排单元格补位
	// （管道数变多），去骨架后按单元格内容序列判定归属
	stripTable := func(s string) string {
		var bd strings.Builder
		bd.Grow(len(s))
		for _, r := range s {
			if r == ' ' || r == '\n' || r == '\t' || r == '\r' || r == '|' || r == '-' {
				continue
			}
			bd.WriteRune(r)
		}
		return bd.String()
	}

	// count 返回严格对齐 / 仅空白差异 / 表格裂分片段 / 真错位 / 越界
	// 表格裂分片段：protectMarkdown 超长表格预切分会重排子表内容（补位空列、
	// 列重组），完整片段不再是父块子串——用去骨架首窗判定归属（尾窗可跨块
	// 撞词，仅作参考不进判定）。
	count := func(pars, chs []SplitResult) (aligned, wsOnly, tblSplit, wrong, oor int) {
		sqPars := make([]string, len(pars))
		tbPars := make([]string, len(pars))
		for i := range pars {
			sqPars[i] = squash(pars[i].Content)
			tbPars[i] = stripTable(pars[i].Content)
		}
		for _, c := range chs {
			if c.ParentIndex < 0 || c.ParentIndex >= len(pars) {
				oor++
				continue
			}
			piece := rawPiece(c)
			if piece == "" || strings.Contains(pars[c.ParentIndex].Content, piece) {
				aligned++
				continue
			}
			if strings.Contains(sqPars[c.ParentIndex], squash(piece)) {
				wsOnly++
				continue
			}
			if strings.HasPrefix(strings.TrimSpace(piece), "|") {
				sp := stripTable(piece)
				rp := []rune(sp)
				if strings.Contains(tbPars[c.ParentIndex], string(rp[:minInt(15, len(rp))])) {
					tblSplit++
					continue
				}
				wrong++
				continue
			}
			if strings.Contains(tbPars[c.ParentIndex], stripTable(piece)) {
				tblSplit++
				continue
			}
			wrong++
		}
		return
	}

	// 真错位样本：输出子块切片与其实际包含父块（去骨架全库搜索）的对照
	dumpWrong := func(pars, chs []SplitResult) {
		tbPars := make([]string, len(pars))
		for i := range pars {
			tbPars[i] = stripTable(pars[i].Content)
		}
		shown := 0
		for i, c := range chs {
			if c.ParentIndex < 0 || c.ParentIndex >= len(pars) {
				continue
			}
			piece := rawPiece(c)
			if piece == "" || strings.Contains(pars[c.ParentIndex].Content, piece) ||
				strings.Contains(tbPars[c.ParentIndex], stripTable(piece)) {
				continue
			}
			// 去骨架后全库搜索真正所属父块
			found := -1
			for pi := range pars {
				if strings.Contains(tbPars[pi], stripTable(piece)) {
					found = pi
					break
				}
			}
			w("--- 真错位样本 %d：子块[%d] ParentIndex=%d", shown, i, c.ParentIndex)
			if found >= 0 {
				w("  实际内容属于父块 %d（去骨架匹配），声称父块 %d", found, c.ParentIndex)
			} else {
				// 探测：取片段去骨架后首/尾 15 rune 窗口，全库找所属
				sp := stripTable(piece)
				rp := []rune(sp)
				head := string(rp[:minInt(15, len(rp))])
				tail := string(rp[maxInt(0, len(rp)-15):])
				headIn, tailIn := -1, -1
				for pi := range pars {
					if strings.Contains(tbPars[pi], head) && headIn < 0 {
						headIn = pi
					}
					if strings.Contains(tbPars[pi], tail) && tailIn < 0 {
						tailIn = pi
					}
				}
				w("  全库均不包含完整片段；首窗 %q → 父块 %d；尾窗 %q → 父块 %d；声称父块 %d",
					head, headIn, tail, tailIn, c.ParentIndex)
			}
			shown++
			if shown >= 5 {
				break
			}
		}
	}

	oA, oWs, oTb, oW, oO := count(oldParents, oldChildren)
	nA, nWs, nTb, nW, nO := count(newParents, newChildren)

	w("")
	w("## 旧管道（先切后 MPT，修复前）")
	w("- 父块 %d，子块 %d；严格对齐 %d，仅空白差异 %d，表格裂分片段 %d，真错位 %d，越界 %d", len(oldParents), len(oldChildren), oA, oWs, oTb, oW, oO)
	w("")
	w("## 新管道（预剥离后）")
	w("- 父块 %d，子块 %d；严格对齐 %d，仅空白差异 %d，表格裂分片段 %d，真错位 %d，越界 %d", len(newParents), len(newChildren), nA, nWs, nTb, nW, nO)

	// 真错位样本（写报告前输出，保证落盘）
	dumpWrong(newParents, newChildren)

	// 等价性：父块逐块一致；子块逐块一致（旧管道的幽灵子块已被其自身 MPT 丢弃）
	sameParents := len(oldParents) == len(newParents)
	if sameParents {
		for i := range oldParents {
			if oldParents[i].Content != newParents[i].Content {
				sameParents = false
				break
			}
		}
	}
	sameChildren := len(oldChildren) == len(newChildren)
	if sameChildren {
		for i := range oldChildren {
			if oldChildren[i].Content != newChildren[i].Content {
				sameChildren = false
				break
			}
		}
	}
	w("")
	w("## 新旧等价性")
	w("- 父块逐块 Content 等价=%v（%d/%d）", sameParents, len(oldParents), len(newParents))
	w("- 子块逐块 Content 等价=%v（%d/%d）", sameChildren, len(oldChildren), len(newChildren))

	outPath := os.Getenv("REALDOC_OUT")
	if outPath == "" {
		outPath = `D:\go_project\chunk_compare\realdoc_parentindex_fix.md`
	}
	if err := os.WriteFile(outPath, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("写报告失败: %v", err)
	}
	t.Logf("报告已写入 %s", outPath)

	// 失败断言放在报告写盘之后；空白差异/表格裂分片段不算错位
	if nW > 0 || nO > 0 || tocRemnants > 0 {
		t.Fatalf("新管道仍有真错位 %d / 越界 %d / 目录残片 %d（空白差异 %d、表格裂分片段 %d 属切分器正常重排）",
			nW, nO, tocRemnants, nWs, nTb)
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
