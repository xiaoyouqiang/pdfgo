package split

// strategy_test.go 覆盖 SplitDocumentTrunks 的标题/平铺双路径策略：
// 标题结构可用→父子关联切分（MakePathTrunk 路径标题）；不可用→平铺定长
// 递归回退（无父块、内容不丢失）。

import (
	"strings"
	"testing"
)

// headingDoc 构造带 3 个二级标题的 markdown
func headingDoc() string {
	var b strings.Builder
	b.WriteString("# 文档标题\n")
	b.WriteString("## 一、概述\n这是概述段落，介绍文档的整体背景与目标。\n\n")
	b.WriteString("## 二、实施方案\n这是实施方案段落，包含具体的执行步骤与责任分工。\n\n")
	b.WriteString("## 三、保障措施\n这是保障措施段落，说明资源、进度与风险控制安排。\n")
	return b.String()
}

// flatDoc 构造无标题的多段落 markdown
func flatDoc() string {
	var b strings.Builder
	for i := 0; i < 10; i++ {
		b.WriteString(strings.Repeat("这是一段没有标题结构的正文内容，用于验证平铺回退路径。", 3))
		b.WriteString("\n\n")
	}
	return b.String()
}

// parse helper：Parse + ClearSplitResult
func mustParse(t *testing.T, md string) []SplitResult {
	t.Helper()
	model := NewSplitModel(nil, true, true, 1000)
	parents := model.Parse(md)
	return model.ClearSplitResult(parents)
}

// TestSplitDocumentTrunks_HeadingPath 标题文档：走标题路径，父块带路径标题，
// 子块 ParentIndex 正确关联，内容无丢失。
func TestSplitDocumentTrunks_HeadingPath(t *testing.T) {
	parents := mustParse(t, headingDoc())
	if !CanSplitByHeading(parents) {
		t.Fatalf("3 个标题的文档应判定可按标题切")
	}

	newParents, children := SplitDocumentTrunks(parents, 384, 0, "测试文档")
	if len(newParents) == 0 {
		t.Fatalf("标题路径父块不应为空")
	}
	if len(children) == 0 {
		t.Fatalf("标题路径子块不应为空")
	}
	// 父块 Title 经 MakePathTrunk 重建为 ## 路径
	foundPath := false
	for _, p := range newParents {
		if strings.Contains(p.Title, "## 一、概述") {
			foundPath = true
		}
	}
	if !foundPath {
		t.Fatalf("父块 Title 应含 ## 路径标题")
	}
	// 子块 ParentIndex 落在父块范围内
	for i, c := range children {
		if c.ParentIndex < 0 || c.ParentIndex >= len(newParents) {
			t.Fatalf("子块 %d ParentIndex 越界: %d", i, c.ParentIndex)
		}
	}
	// 内容无丢失：三个章节正文都出现在子块中
	joined := ""
	for _, c := range children {
		joined += c.Content
	}
	for _, kw := range []string{"概述段落", "实施方案段落", "保障措施段落"} {
		if !strings.Contains(joined, kw) {
			t.Fatalf("子块内容丢失关键词 %q", kw)
		}
	}
}

// TestSplitDocumentTrunks_FlatFallback 无标题文档：走平铺回退，无父块、
// 内容不丢失、子块无父子关联。
func TestSplitDocumentTrunks_FlatFallback(t *testing.T) {
	parents := mustParse(t, flatDoc())
	if CanSplitByHeading(parents) {
		t.Fatalf("无标题文档不应判定可按标题切")
	}

	newParents, children := SplitDocumentTrunks(parents, 384, 0, "测试文档")
	if len(newParents) != 0 {
		t.Fatalf("平铺回退父块应为空，实际 %d", len(newParents))
	}
	if len(children) == 0 {
		t.Fatalf("平铺回退子块不应为空")
	}
	// 内容无丢失：原文全部正文出现在子块并集中
	joined := strings.Join(func() []string {
		out := make([]string, 0, len(children))
		for _, c := range children {
			out = append(out, c.Content)
		}
		return out
	}(), "")
	if !strings.Contains(joined, strings.Repeat("这是一段没有标题结构的正文内容，用于验证平铺回退路径。", 3)) {
		t.Fatalf("平铺回退内容丢失")
	}
	// 平铺子块不拼路径标题（无 "##" 前缀行）
	for _, c := range children {
		if strings.HasPrefix(strings.TrimSpace(c.Content), "##") {
			t.Fatalf("平铺子块不应带标题路径: %.60q", c.Content)
		}
	}
}

// TestSplitDocumentTrunks_WeakHeading 仅 1-2 个标题块的弱标题文档走平铺回退。
func TestSplitDocumentTrunks_WeakHeading(t *testing.T) {
	md := "# 文档标题\n正文段落一。\n\n正文段落二。\n\n正文段落三。"
	parents := mustParse(t, md)
	if CanSplitByHeading(parents) {
		t.Fatalf("仅 1 个标题块不应判定可按标题切")
	}
	newParents, children := SplitDocumentTrunks(parents, 384, 0, "")
	if len(newParents) != 0 {
		t.Fatalf("弱标题文档应走平铺回退（父块为空）")
	}
	if len(children) == 0 {
		t.Fatalf("弱标题文档平铺回退子块不应为空")
	}
}

// TestSplitDocumentTrunks_ZeroChunkSize chunkSize≤0：children=nil，
// 标题路径 parents 仍经 MakePathTrunk。
func TestSplitDocumentTrunks_ZeroChunkSize(t *testing.T) {
	headingParents := mustParse(t, headingDoc())
	newParents, children := SplitDocumentTrunks(headingParents, 0, 0, "测试文档")
	if children != nil {
		t.Fatalf("chunkSize=0 时子块应为 nil")
	}
	if len(newParents) == 0 {
		t.Fatalf("chunkSize=0 时标题路径父块不应为空")
	}

	flatParents := mustParse(t, flatDoc())
	flatP, flatC := SplitDocumentTrunks(flatParents, 0, 0, "")
	if flatP != nil || flatC != nil {
		t.Fatalf("chunkSize=0 时平铺回退应返回 nil,nil")
	}
}

// TestSplitDocumentTrunks_SingleBlock 单块无标题文档：平铺出 ≥1 块。
func TestSplitDocumentTrunks_SingleBlock(t *testing.T) {
	parents := []SplitResult{{Content: "只有一个段落的无标题内容。"}}
	newParents, children := SplitDocumentTrunks(parents, 384, 0, "")
	if len(newParents) != 0 {
		t.Fatalf("单块无标题父块应为空")
	}
	if len(children) != 1 || children[0].Content != parents[0].Content {
		t.Fatalf("单块文档平铺应原样保留，实际 %d 块", len(children))
	}
}

// assertChildAligned 逐子块断言 ParentIndex 精确对齐：子块内容必须出现在
// 其所指父块的内容里（父块 Content 经 MakePathTrunk 前置路径标题后仍包含原文）。
func assertChildAligned(t *testing.T, parents, children []SplitResult) {
	t.Helper()
	for i, c := range children {
		if c.ParentIndex < 0 || c.ParentIndex >= len(parents) {
			t.Fatalf("子块 %d ParentIndex 越界: %d（父块数 %d）", i, c.ParentIndex, len(parents))
		}
		if !strings.Contains(parents[c.ParentIndex].Content, c.Content) {
			t.Fatalf("子块 %d 内容未出现在所指父块 %d 中，父子下标错位。\n子块: %.60q\n父块: %.60q",
				i, c.ParentIndex, c.Content, parents[c.ParentIndex].Content)
		}
	}
}

// TestSplitDocumentTrunks_AlignedWithH1DocTitle 真实场景：H1 == docTitle
// （≥10 字被 ClearSplitResult 保留为 content==docTitle 的块）。预剥离后
// 父块不含该块，且每个子块按内容挂到正确父块。
func TestSplitDocumentTrunks_AlignedWithH1DocTitle(t *testing.T) {
	// 同级标题紧跟封面标题（doc 106 的真实结构）：封面 H1 无 children →
	// 产 title-only 块 → ClearSplitResult 后 content==docTitle
	md := "# 质量体系策划的管理程序\n" +
		"# 一、概述\n这是概述段落，介绍文档的整体背景与目标。\n\n" +
		"# 二、实施方案\n这是实施方案段落，包含具体的执行步骤与责任分工。\n\n" +
		"# 三、保障措施\n这是保障措施段落，说明资源、进度与风险控制安排。\n"
	parents := mustParse(t, md)
	// 前置条件：Parse 产物确实存在 content==docTitle 的块（否则用例失效）
	found := false
	for _, p := range parents {
		if strings.TrimSpace(p.Content) == "质量体系策划的管理程序" {
			found = true
		}
	}
	if !found {
		t.Fatalf("前置条件不满足：Parse 产物中无 content==docTitle 的块")
	}

	newParents, children := SplitDocumentTrunks(parents, 384, 0, "质量体系策划的管理程序")
	if len(newParents) != 3 {
		t.Fatalf("剥离 docTitle 块后父块应为 3，实际 %d", len(newParents))
	}
	if len(children) == 0 {
		t.Fatalf("子块不应为空")
	}
	assertChildAligned(t, newParents, children)
	// 指向正确父块：含概述正文的子块必须挂在「一、概述」路径的父块上
	for _, c := range children {
		if strings.Contains(c.Content, "概述段落") &&
			!strings.Contains(newParents[c.ParentIndex].Title, "一、概述") {
			t.Fatalf("概述子块挂错父块: %q", newParents[c.ParentIndex].Title)
		}
	}
}

// TestSplitDocumentTrunks_AlignedWithMiddleDocTitleBlock content==docTitle
// 的块位于数组中部：剥离后其后所有子块下标前移，必须仍精确对齐。
// （未修复的实现在此用例下：子块按 4 块数组编下标、父块剩 3 块，错位一格。）
func TestSplitDocumentTrunks_AlignedWithMiddleDocTitleBlock(t *testing.T) {
	parents := []SplitResult{
		{Title: "## 一", ParentChain: []string{"一"}, Content: "第一个块正文，内容完整未超阈值。"},
		{Title: "## 文档名", ParentChain: []string{"质量体系策划的管理程序"}, Content: "质量体系策划的管理程序"},
		{Title: "## 三", ParentChain: []string{"三"}, Content: "第三个块正文，内容完整未超阈值。"},
		{Title: "## 四", ParentChain: []string{"四"}, Content: "第四个块正文，内容完整未超阈值。"},
	}
	newParents, children := SplitDocumentTrunks(parents, 384, 0, "质量体系策划的管理程序")
	if len(newParents) != 3 {
		t.Fatalf("剥离中部 docTitle 块后父块应为 3，实际 %d", len(newParents))
	}
	if len(children) != 3 {
		t.Fatalf("剥离后子块应为 3（docTitle 块不再产子块），实际 %d", len(children))
	}
	assertChildAligned(t, newParents, children)
}

// TestSplitDocumentTrunks_KeepEmptyContentWithChain 空 content 但
// ParentChain 非空的块：MakePathTrunk 会回退 content=路径标题并保留，
// 预剥离不得错删（否则又引入另一种错位）。
func TestSplitDocumentTrunks_KeepEmptyContentWithChain(t *testing.T) {
	parents := []SplitResult{
		{Title: "## 一", ParentChain: []string{"一"}, Content: "第一个块正文，内容完整未超阈值。"},
		{Title: "## 二", ParentChain: []string{"二"}, Content: ""},
		{Title: "## 三", ParentChain: []string{"三"}, Content: "第三个块正文，内容完整未超阈值。"},
		{Title: "## 四", ParentChain: []string{"四"}, Content: "第四个块正文，内容完整未超阈值。"},
	}
	newParents, children := SplitDocumentTrunks(parents, 384, 0, "质量体系策划的管理程序")
	if len(newParents) != 4 {
		t.Fatalf("空 content 但有 ParentChain 的块应保留，父块应为 4，实际 %d", len(newParents))
	}
	if len(children) != 4 {
		t.Fatalf("子块应为 4，实际 %d", len(children))
	}
	assertChildAligned(t, newParents, children)
}
