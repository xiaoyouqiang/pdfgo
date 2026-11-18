// Package split 提供 Markdown 文本的智能分段功能。
//
// 本文件实现文档级切分策略：先判断一级分段结果是否具备可用的标题结构，
// 可用则按标题路径做二级切分（父子块关联），不可用则回退为固定长度
// 递归切分（平铺子块、无父块）。借鉴 WeKnora chunker 的 heading→legacy
// 分层回退策略（profiler.SelectStrategy + Split 分层校验回退）。
package split

import "strings"

// CanSplitByHeading 判断一级分段结果是否具备可按标题切分的结构。
//
// 门槛对齐 WeKnora SelectStrategy（块口径）：
//   - 标题块数 ≥ 3（绝对数下限，防极短文档误判）
//   - 标题块占比 > 0.5%（密度下限，防"海量碎块 + 个别标题"的退化文档
//     走标题路径产生巨型父块）
//
// 不满足任一条件时返回 false，调用方应走平铺递归回退。
func CanSplitByHeading(parents []SplitResult) bool {
	if len(parents) == 0 {
		return false
	}
	titled := 0
	for _, r := range parents {
		if strings.TrimSpace(r.Title) != "" {
			titled++
		}
	}
	if titled < 3 {
		return false
	}
	return float64(titled)/float64(len(parents)) > 0.005
}

// SplitDocumentTrunks 统一的二级切分入口：按标题结构可用性选择切分路径。
//
//   - 可按标题（CanSplitByHeading == true）：SplitLargeBlocks(…, true) 切出
//     父子关联子块（子块 ParentIndex 指向父块下标），parents/children 各经
//     MakePathTrunk(docTitle) 重建标题路径后返回。
//   - 不可按标题：SplitLargeBlocks(…, false) 切出平铺子块（表格/链接保护、
//     无父子关联），parents 返回 nil，children 不做 MakePathTrunk——对齐
//     txt/pptx 平铺路径的返回契约（Title 为空时 MakePathTrunk 会引入前导换行）。
//   - chunkSize ≤ 0：两路径 children 均返回 nil（维持历史行为），标题路径
//     parents 仍经 MakePathTrunk。
//
// 标题路径先经 dropPathTrunkRemoved 预剥离，保证 SplitLargeBlocks 编出的
// 子块 ParentIndex 与最终返回的父块数组严格对齐（落库按 parentTrunks[ParentIndex]
// 取父块）。
func SplitDocumentTrunks(parents []SplitResult, chunkSize, tableSplitThreshold int, docTitle string) (parentsOut, children []SplitResult) {
	if !CanSplitByHeading(parents) {
		if chunkSize <= 0 {
			return nil, nil
		}
		return nil, SplitLargeBlocks(parents, chunkSize, tableSplitThreshold, false)
	}

	parents = dropPathTrunkRemoved(parents, docTitle)

	var chunks []SplitResult
	if chunkSize > 0 {
		chunks = SplitLargeBlocks(parents, chunkSize, tableSplitThreshold, true)
	}
	return MakePathTrunk(docTitle, parents), MakePathTrunk(docTitle, chunks)
}

// dropPathTrunkRemoved 预剥离 MakePathTrunk 会丢弃的块，使父子下标对齐。
//
// 若剥离发生在二级切分之后（MakePathTrunk 内部），父块数组缩短而子块
// ParentIndex 仍按剥离前的下标编写，落库 parentTrunks[ParentIndex] 自丢弃
// 位置起整体错位一格。这里把等价的保留/丢弃判定前置到 SplitLargeBlocks
// 之前：切分的下标基准 == 最终返回数组，且此后 MakePathTrunk 无块可丢。
//
// 判定与 MakePathTrunk 的语义逐条对应：
//   - TrimSpace(content) 非空且等于 TrimSpace(docTitle) → 丢弃（根级 H1
//     title-only 的保留产物，content 已被 ClearSplitResult 转为标题文本）；
//   - TrimSpace(content) 为空 → MakePathTrunk 会把 content 回退为重建的
//     路径标题并保留该块（Content 变为标题文本），故保留；仅当 ParentChain
//     为空、重建标题亦为空串时，落入 MakePathTrunk 末尾 content != "" 守卫
//     被丢弃。
func dropPathTrunkRemoved(parents []SplitResult, docTitle string) []SplitResult {
	dt := strings.TrimSpace(docTitle)
	out := make([]SplitResult, 0, len(parents))
	for _, item := range parents {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			// 空 content：MPT 回退为路径标题后保留；仅 ParentChain 为空时被丢
			if len(item.ParentChain) == 0 {
				continue
			}
			out = append(out, item)
			continue
		}
		if dt != "" && content == dt {
			continue
		}
		out = append(out, item)
	}
	return out
}
