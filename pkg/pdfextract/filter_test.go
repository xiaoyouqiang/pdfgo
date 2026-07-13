package pdfextract

import (
	"testing"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/model"
)

// helper：构造一页包含若干 TextBox，每个 TextBox 由多行文本组成
func makePage(pageNum int, lines ...[]string) model.Page {
	var tbs []model.TextBox
	for _, ls := range lines {
		tb := model.TextBox{}
		for _, s := range ls {
			tl := model.TextLine{}
			for _, r := range s {
				tl.Chars = append(tl.Chars, model.Char{Text: string(r)})
			}
			tb.Lines = append(tb.Lines, tl)
		}
		tbs = append(tbs, tb)
	}
	return model.Page{PageNum: pageNum, TextBoxes: tbs}
}

// 收集一页所有行文本，便于断言
func pageLines(p model.Page) []string {
	var out []string
	for _, tb := range p.TextBoxes {
		for _, l := range tb.Lines {
			out = append(out, l.Text())
		}
	}
	return out
}

func TestApplyLineFilters_Empty(t *testing.T) {
	pages := []model.Page{makePage(1, []string{"目录", "正文"})}
	before := len(pages[0].TextBoxes)
	ApplyLineFilters(pages, nil)
	if len(pages[0].TextBoxes) != before {
		t.Errorf("nil filters should be no-op: got %d TextBoxes, want %d", len(pages[0].TextBoxes), before)
	}
	ApplyLineFilters(pages, []LineFilter{})
	if len(pages[0].TextBoxes) != before {
		t.Errorf("empty filters should be no-op: got %d TextBoxes, want %d", len(pages[0].TextBoxes), before)
	}
}

func TestApplyLineFilters_SubstringSpecificPage(t *testing.T) {
	pages := []model.Page{
		makePage(1, []string{"目录", "正文A"}),
		makePage(2, []string{"目录", "正文B"}),
	}
	ApplyLineFilters(pages, []LineFilter{
		{Pages: []int{1}, Contains: []string{"目录"}},
	})
	if got := pageLines(pages[0]); len(got) != 1 || got[0] != "正文A" {
		t.Errorf("page1: got %v, want [正文A]", got)
	}
	if got := pageLines(pages[1]); len(got) != 2 {
		t.Errorf("page2: should be untouched, got %v", got)
	}
}

func TestApplyLineFilters_WildcardPages(t *testing.T) {
	pages := []model.Page{
		makePage(1, []string{"CONFIDENTIAL", "A"}),
		makePage(2, []string{"B", "CONFIDENTIAL"}),
	}
	ApplyLineFilters(pages, []LineFilter{
		{Contains: []string{"CONFIDENTIAL"}}, // Pages 空 = 所有页
	})
	if got := pageLines(pages[0]); len(got) != 1 || got[0] != "A" {
		t.Errorf("page1: got %v, want [A]", got)
	}
	if got := pageLines(pages[1]); len(got) != 1 || got[0] != "B" {
		t.Errorf("page2: got %v, want [B]", got)
	}
}

func TestApplyLineFilters_Regex(t *testing.T) {
	pages := []model.Page{
		makePage(1, []string{"第1页", "正文", "第12页"}),
	}
	ApplyLineFilters(pages, []LineFilter{
		{Pages: []int{1}, Regex: []string{`^第\d+页$`}},
	})
	got := pageLines(pages[0])
	if len(got) != 1 || got[0] != "正文" {
		t.Errorf("regex filter: got %v, want [正文]", got)
	}
}

func TestApplyLineFilters_FilterBox(t *testing.T) {
	// TextBox 有 3 行，其中 1 行命中 → 整 Box 删除
	pages := []model.Page{
		makePage(1, []string{"保留行1", "BADGER", "保留行2"}, []string{"另一Box"}),
	}
	ApplyLineFilters(pages, []LineFilter{
		{Pages: []int{1}, Contains: []string{"BADGER"}, Scope: FilterBox},
	})
	got := pageLines(pages[0])
	if len(got) != 1 || got[0] != "另一Box" {
		t.Errorf("FilterBox: got %v, want [另一Box]", got)
	}
}

func TestApplyLineFilters_FilterLineKeepsOtherLines(t *testing.T) {
	// 同一 Box 内只删命中行，其他行保留
	pages := []model.Page{
		makePage(1, []string{"保留1", "删除我", "保留2"}),
	}
	ApplyLineFilters(pages, []LineFilter{
		{Pages: []int{1}, Contains: []string{"删除"}, Scope: FilterLine},
	})
	got := pageLines(pages[0])
	if len(got) != 2 || got[0] != "保留1" || got[1] != "保留2" {
		t.Errorf("FilterLine: got %v, want [保留1 保留2]", got)
	}
}

func TestApplyLineFilters_EmptyTextBoxDropped(t *testing.T) {
	// 整 Box 所有行被删 → TextBox 也被丢弃
	pages := []model.Page{
		makePage(1, []string{"删除1", "删除2"}, []string{"保留"}),
	}
	ApplyLineFilters(pages, []LineFilter{
		{Pages: []int{1}, Contains: []string{"删除"}, Scope: FilterLine},
	})
	if len(pages[0].TextBoxes) != 1 {
		t.Errorf("empty TextBox should be dropped: got %d TextBoxes", len(pages[0].TextBoxes))
	}
	got := pageLines(pages[0])
	if len(got) != 1 || got[0] != "保留" {
		t.Errorf("got %v, want [保留]", got)
	}
}

func TestApplyLineFilters_InvalidRegexSilentlySkipped(t *testing.T) {
	pages := []model.Page{
		makePage(1, []string{"正文", "目标"}),
	}
	// 无效正则 + 有效 Contains：规则仍然因 Contains 生效
	ApplyLineFilters(pages, []LineFilter{
		{Pages: []int{1}, Regex: []string{"(invalid"}, Contains: []string{"目标"}},
	})
	got := pageLines(pages[0])
	if len(got) != 1 || got[0] != "正文" {
		t.Errorf("invalid regex should not break Contains: got %v, want [正文]", got)
	}
}

func TestApplyLineFilters_PageNumFallback(t *testing.T) {
	// PageNum=0 时应回退到 pi+1
	pages := []model.Page{
		{PageNum: 0, TextBoxes: []model.TextBox{
			{Lines: []model.TextLine{mkLine("目录")}},
		}},
	}
	ApplyLineFilters(pages, []LineFilter{
		{Pages: []int{1}, Contains: []string{"目录"}},
	})
	if len(pages[0].TextBoxes) != 0 {
		t.Errorf("PageNum=0 should fall back to index+1: got %d TextBoxes", len(pages[0].TextBoxes))
	}
}

func TestApplyLineFilters_MultipleFiltersOR(t *testing.T) {
	pages := []model.Page{
		makePage(1, []string{"A", "B", "C", "D"}),
	}
	// 两条规则：删 B 和 C
	ApplyLineFilters(pages, []LineFilter{
		{Pages: []int{1}, Contains: []string{"B"}},
		{Pages: []int{1}, Contains: []string{"C"}},
	})
	got := pageLines(pages[0])
	if len(got) != 2 || got[0] != "A" || got[1] != "D" {
		t.Errorf("multiple filters: got %v, want [A D]", got)
	}
}

func TestApplyLineFilters_BoxScopeAcrossBoxes(t *testing.T) {
	// 命中第 2 个 Box 的某行 → 只删第 2 个 Box
	pages := []model.Page{
		makePage(1, []string{"保留1", "保留2"}, []string{"命中", "也保留"}, []string{"第三个"}),
	}
	ApplyLineFilters(pages, []LineFilter{
		{Pages: []int{1}, Contains: []string{"命中"}, Scope: FilterBox},
	})
	if len(pages[0].TextBoxes) != 2 {
		t.Fatalf("expected 2 TextBoxes, got %d", len(pages[0].TextBoxes))
	}
	got := pageLines(pages[0])
	if got[0] != "保留1" || got[1] != "保留2" || got[2] != "第三个" {
		t.Errorf("got %v, want [保留1 保留2 第三个]", got)
	}
}

func mkLine(s string) model.TextLine {
	tl := model.TextLine{}
	for _, r := range s {
		tl.Chars = append(tl.Chars, model.Char{Text: string(r)})
	}
	return tl
}

// 构造一个 Table，cells 按行传入（每行字符串切片）
func makeTable(cells [][]string) model.Table {
	rows := len(cells)
	cols := 0
	for _, r := range cells {
		if len(r) > cols {
			cols = len(r)
		}
	}
	tbl := model.Table{Rows: rows, Cols: cols, Cells: make([][]model.Cell, rows)}
	for ri, row := range cells {
		tbl.Cells[ri] = make([]model.Cell, len(row))
		for ci, text := range row {
			tbl.Cells[ri][ci] = model.Cell{Text: text, Row: ri, Col: ci}
		}
	}
	return tbl
}

func TestApplyLineFilters_TableSubstring(t *testing.T) {
	pages := []model.Page{{
		PageNum: 1,
		Tables: []model.Table{
			makeTable([][]string{{"文件名称", "生物之芯"}, {"文件编号", "SS-SOP"}}),
			makeTable([][]string{{"姓名", "张三"}, {"年龄", "30"}}),
		},
	}}
	ApplyLineFilters(pages, []LineFilter{
		{Contains: []string{"文件名称"}, Target: TargetTable},
	})
	if len(pages[0].Tables) != 1 {
		t.Fatalf("expected 1 table left, got %d", len(pages[0].Tables))
	}
	first := pages[0].Tables[0].Cells[0][0].Text
	if first != "姓名" {
		t.Errorf("remaining table should be the one without 文件名称; got first cell %q", first)
	}
}

func TestApplyLineFilters_TableRegex(t *testing.T) {
	pages := []model.Page{{
		PageNum: 2,
		Tables: []model.Table{
			makeTable([][]string{{"版本"}, {"A/0"}}),
			makeTable([][]string{{"日期"}, {"2024"}}),
		},
	}}
	ApplyLineFilters(pages, []LineFilter{
		{Pages: []int{2}, Regex: []string{`^版本$`}, Target: TargetTable},
	})
	if len(pages[0].Tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(pages[0].Tables))
	}
	if pages[0].Tables[0].Cells[0][0].Text != "日期" {
		t.Errorf("wrong table kept: %q", pages[0].Tables[0].Cells[0][0].Text)
	}
}

func TestApplyLineFilters_TablePageScoped(t *testing.T) {
	t1 := makeTable([][]string{{"X"}})
	t2 := makeTable([][]string{{"X"}})
	pages := []model.Page{
		{PageNum: 1, Tables: []model.Table{t1}},
		{PageNum: 2, Tables: []model.Table{t2}},
	}
	ApplyLineFilters(pages, []LineFilter{
		{Pages: []int{1}, Contains: []string{"X"}, Target: TargetTable},
	})
	if len(pages[0].Tables) != 0 || len(pages[1].Tables) != 1 {
		t.Errorf("page-scoped table filter: p1=%d p2=%d, want 0 and 1", len(pages[0].Tables), len(pages[1].Tables))
	}
}

func TestApplyLineFilters_TableAndTextBoxIndependent(t *testing.T) {
	// 同时配置 TextBox 和 Table 规则，验证互不干扰
	pages := []model.Page{{
		PageNum: 1,
		TextBoxes: []model.TextBox{{Lines: []model.TextLine{mkLine("噪声文本")}}},
		Tables:    []model.Table{makeTable([][]string{{"噪声表格"}})},
	}}
	ApplyLineFilters(pages, []LineFilter{
		{Contains: []string{"噪声文本"}},                                       // TextBox
		{Contains: []string{"噪声表格"}, Target: TargetTable},                  // Table
	})
	if len(pages[0].TextBoxes) != 0 {
		t.Errorf("TextBox should be dropped, got %d", len(pages[0].TextBoxes))
	}
	if len(pages[0].Tables) != 0 {
		t.Errorf("Table should be dropped, got %d", len(pages[0].Tables))
	}
}

func TestApplyLineFilters_TableNoHitKeepsAll(t *testing.T) {
	pages := []model.Page{{
		PageNum: 1,
		Tables: []model.Table{
			makeTable([][]string{{"A"}}),
			makeTable([][]string{{"B"}}),
		},
	}}
	ApplyLineFilters(pages, []LineFilter{
		{Contains: []string{"Z"}, Target: TargetTable},
	})
	if len(pages[0].Tables) != 2 {
		t.Errorf("no hit should keep all tables: got %d", len(pages[0].Tables))
	}
}
