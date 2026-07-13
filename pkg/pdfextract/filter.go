package pdfextract

import (
	"regexp"
	"strings"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/model"
)

// FilterScope 控制命中过滤规则时删除的粒度（仅 Target=TargetTextBox 时生效）。
type FilterScope int

const (
	// FilterLine 仅删除命中的单行（默认）。
	FilterLine FilterScope = iota
	// FilterBox 删除命中行所在的整个 TextBox。
	FilterBox
)

// FilterTarget 控制规则作用于哪一类对象。
type FilterTarget int

const (
	// TargetTextBox 默认：规则作用于 TextBox.Lines，命中后按 Scope 删除行或整个 TextBox。
	TargetTextBox FilterTarget = iota
	// TargetTable 规则作用于 Table：表格中任一单元格文本命中即删除整个 Table（Scope 字段被忽略）。
	TargetTable
)

// LineFilter 定义按页码过滤文本的规则。
//
// TextBox 模式（默认）：
//   - 行命中当且仅当：页码匹配 且 文本包含 Contains 任一子串或匹配 Regex 任一正则
//   - 命中后由 Scope 决定删行（FilterLine）或删整个 TextBox（FilterBox）
//
// Table 模式（Target=TargetTable）：
//   - 表格中任一 Cell.Text 命中即删除整个 Table（Scope 字段被忽略）
type LineFilter struct {
	Pages    []int        // 1-based 页码列表；nil/空表示所有页
	Contains []string     // 子串匹配（OR 关系）
	Regex    []string     // 正则匹配（OR 关系），无效正则静默跳过
	Scope    FilterScope  // 仅 Target=TargetTextBox 时生效；零值 FilterLine
	Target   FilterTarget // 作用于 TextBox 还是 Table；零值 TargetTextBox
}

type compiledFilter struct {
	pages    map[int]bool // 空 map 表示匹配所有页
	allPages bool
	contains []string
	regex    []*regexp.Regexp
	scope    FilterScope
	target   FilterTarget
}

// ApplyLineFilters 按规则 in-place 修改 pages：
//   - Target=TargetTextBox：从 page.TextBoxes 删除命中行/TextBox，空的 TextBox 自动丢弃
//   - Target=TargetTable：从 page.Tables 删除任一单元格命中的 Table
//
// filters 为空时直接返回，不做任何修改。
func ApplyLineFilters(pages []model.Page, filters []LineFilter) {
	if len(filters) == 0 || len(pages) == 0 {
		return
	}

	var tbFilters, tblFilters []compiledFilter
	for _, f := range filters {
		cf := compileFilter(f)
		// 仅当至少有一个文本匹配条件才生效，否则规则无意义
		if len(cf.contains) == 0 && len(cf.regex) == 0 {
			continue
		}
		if cf.target == TargetTable {
			tblFilters = append(tblFilters, cf)
		} else {
			tbFilters = append(tbFilters, cf)
		}
	}
	if len(tbFilters) == 0 && len(tblFilters) == 0 {
		return
	}

	for pi := range pages {
		pageNum := pages[pi].PageNum
		if pageNum <= 0 {
			pageNum = pi + 1
		}

		// 1. 过滤 TextBoxes
		if len(tbFilters) > 0 {
			var filtered []model.TextBox
			for _, tb := range pages[pi].TextBoxes {
				dropBox := false
				var keep []model.TextLine
				for _, line := range tb.Lines {
					text := line.Text()
					if cf, scope := matchAny(pageNum, text, tbFilters); cf != nil {
						if scope == FilterBox {
							dropBox = true
							break // 整个 Box 不要，无需继续扫描
						}
						// FilterLine：不追加到 keep
						continue
					}
					keep = append(keep, line)
				}
				if dropBox {
					continue
				}
				if len(keep) > 0 {
					tb.Lines = keep
					filtered = append(filtered, tb)
				}
			}
			pages[pi].TextBoxes = filtered
		}

		// 2. 过滤 Tables：任一 Cell 命中 → 整个 Table 删除
		if len(tblFilters) > 0 && len(pages[pi].Tables) > 0 {
			var keptTables []model.Table
			for _, tbl := range pages[pi].Tables {
				if tableHit(pageNum, tbl, tblFilters) {
					continue
				}
				keptTables = append(keptTables, tbl)
			}
			pages[pi].Tables = keptTables
		}
	}
}

func compileFilter(f LineFilter) compiledFilter {
	cf := compiledFilter{scope: f.Scope, target: f.Target}
	if len(f.Pages) == 0 {
		cf.allPages = true
	} else {
		cf.pages = make(map[int]bool, len(f.Pages))
		for _, p := range f.Pages {
			cf.pages[p] = true
		}
	}
	cf.contains = f.Contains
	for _, p := range f.Regex {
		re, err := regexp.Compile(p)
		if err != nil {
			continue
		}
		cf.regex = append(cf.regex, re)
	}
	return cf
}

// tableHit 检查表格中是否有任一单元格命中规则。
func tableHit(pageNum int, tbl model.Table, filters []compiledFilter) bool {
	for _, row := range tbl.Cells {
		for _, cell := range row {
			if cf, _ := matchAny(pageNum, cell.Text, filters); cf != nil {
				return true
			}
		}
	}
	return false
}

// matchAny 在已编译的规则列表中查找第一个命中当前页/文本的规则。
// 返回 (规则指针, 规则的 Scope)；未命中返回 (nil, FilterLine)。
func matchAny(pageNum int, text string, filters []compiledFilter) (*compiledFilter, FilterScope) {
	if text == "" {
		return nil, FilterLine
	}
	for i := range filters {
		cf := &filters[i]
		if !cf.allPages && !cf.pages[pageNum] {
			continue
		}
		if matchText(text, cf) {
			return cf, cf.scope
		}
	}
	return nil, FilterLine
}

func matchText(text string, cf *compiledFilter) bool {
	for _, s := range cf.contains {
		if s == "" {
			continue
		}
		if strings.Contains(text, s) {
			return true
		}
	}
	for _, re := range cf.regex {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}
