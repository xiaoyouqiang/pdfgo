package pdfextract

import (
	"regexp"
	"strings"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/model"
)

// FilterScope 控制命中过滤规则时删除的粒度。
type FilterScope int

const (
	// FilterLine 仅删除命中的单行（默认）。
	FilterLine FilterScope = iota
	// FilterBox 删除命中行所在的整个 TextBox。
	FilterBox
)

// LineFilter 定义按页码过滤文本行的规则。
//
// 一行命中规则当且仅当：
//   - 页码匹配：Pages 为空表示所有页，否则须包含当前页 PageNum
//   - 文本匹配：行文本包含 Contains 中任一子串，或匹配 Regex 中任一正则
//
// 命中后的处理由 Scope 决定（FilterLine 删行，FilterBox 删整个 TextBox）。
type LineFilter struct {
	Pages    []int       // 1-based 页码列表；nil/空表示所有页
	Contains []string    // 子串匹配（OR 关系）
	Regex    []string    // 正则匹配（OR 关系），无效正则静默跳过
	Scope    FilterScope // 命中时删除行还是整个 TextBox；零值 FilterLine
}

type compiledFilter struct {
	pages    map[int]bool // 空 map 表示匹配所有页
	allPages bool
	contains []string
	regex    []*regexp.Regexp
	scope    FilterScope
}

// ApplyLineFilters 按规则 in-place 修改 pages：从 page.TextBoxes 中删除命中的行或 TextBox。
//
// 空的 TextBox 会被自动丢弃（与 FilterHeadersFooters 一致）。
// filters 为空时直接返回，不做任何修改。
func ApplyLineFilters(pages []model.Page, filters []LineFilter) {
	if len(filters) == 0 || len(pages) == 0 {
		return
	}

	compiled := make([]compiledFilter, 0, len(filters))
	for _, f := range filters {
		cf := compiledFilter{scope: f.Scope}
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
		// 仅当至少有一个文本匹配条件才生效，否则规则无意义
		if len(cf.contains) == 0 && len(cf.regex) == 0 {
			continue
		}
		compiled = append(compiled, cf)
	}
	if len(compiled) == 0 {
		return
	}

	for pi := range pages {
		pageNum := pages[pi].PageNum
		if pageNum <= 0 {
			pageNum = pi + 1
		}
		var filtered []model.TextBox
		for _, tb := range pages[pi].TextBoxes {
			dropBox := false
			var keep []model.TextLine
			for _, line := range tb.Lines {
				text := line.Text()
				if cf, scope := matchLine(pageNum, text, compiled); cf != nil {
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
}

// matchLine 在已编译的规则列表中查找第一个命中当前页/文本的规则。
// 返回 (规则指针, 规则的 Scope)；未命中返回 (nil, FilterLine)。
func matchLine(pageNum int, text string, filters []compiledFilter) (*compiledFilter, FilterScope) {
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
