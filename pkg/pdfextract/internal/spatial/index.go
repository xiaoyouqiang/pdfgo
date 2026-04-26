// Package spatial 提供空间索引功能，用于快速查询矩形区域内的对象。
package spatial

import "github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/model"

// Index 是一个泛型空间索引，支持按矩形区域查询对象。
// 当前实现为暴力扫描（遍历所有项目），后续可替换为 R-tree 以提升性能。
type Index[T any] struct {
	items []indexItem[T]
}

// indexItem 存储一个带边界框的对象
type indexItem[T any] struct {
	bbox model.Rect
	item T
}

// NewIndex 创建一个新的空间索引
func NewIndex[T any]() *Index[T] {
	return &Index[T]{}
}

// Insert 向索引中插入一个带边界框的对象
func (idx *Index[T]) Insert(bbox model.Rect, item T) {
	idx.items = append(idx.items, indexItem[T]{bbox: bbox, item: item})
}

// Query 查询所有与给定矩形有重叠区域的对象
func (idx *Index[T]) Query(bbox model.Rect) []T {
	var result []T
	for _, item := range idx.items {
		if bbox.Overlap(item.bbox) > 0 {
			result = append(result, item.item)
		}
	}
	return result
}

// Len 返回索引中的对象数量
func (idx *Index[T]) Len() int {
	return len(idx.items)
}
