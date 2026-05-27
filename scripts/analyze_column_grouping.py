#!/usr/bin/env python3
"""分析 PyMuPDF 的列分组逻辑"""

import sys
sys.stdout.reconfigure(encoding='utf-8')

import fitz


def analyze_column_grouping():
    """分析 PyMuPDF 如何按列分组"""
    doc = fitz.open("2605.23261v1.pdf")
    page = doc[0]
    blocks = page.get_text("dict")["blocks"]

    print("第一页所有块的分析：")
    print("="*80)

    # 统计左栏和右栏的块
    left_blocks = []
    right_blocks = []
    center_blocks = []

    for i, block in enumerate(blocks):
        if "lines" not in block:
            continue

        x0, y0, x1, y1 = block["bbox"]
        x_mid = (x0 + x1) / 2

        # 计算行数
        line_count = len(block["lines"])

        print(f"Block {i}: X={x0:.1f}-{x1:.1f} Y={y0:.1f}-{y1:.1f} Lines={line_count} MidX={x_mid:.1f}")

        if x_mid < 250:
            left_blocks.append(i)
        elif x_mid > 350:
            right_blocks.append(i)
        else:
            center_blocks.append(i)

    print(f"\n左栏 (X mid < 250): Blocks {left_blocks}")
    print(f"右栏 (X mid > 350): Blocks {right_blocks}")
    print(f"中间 (250 <= X mid <= 350): Blocks {center_blocks}")

    # 检查是否左栏和右栏的内容是交错排列的
    print("\n" + "="*80)
    print("检查 Block 4 (左栏) 和 Block 9 (右栏) 的 Y 范围重叠：")
    print("="*80)

    block_4 = blocks[4]
    block_9 = blocks[9]

    print(f"Block 4 (左栏Abstract): Y={block_4['bbox'][1]:.1f}-{block_4['bbox'][3]:.1f}")
    print(f"Block 9 (右栏内容): Y={block_9['bbox'][1]:.1f}-{block_9['bbox'][3]:.1f}")

    # 检查 Y 重叠
    y_overlap = min(block_4['bbox'][3], block_9['bbox'][3]) - max(block_4['bbox'][1], block_9['bbox'][1])
    print(f"Y overlap: {y_overlap:.1f}")

    # 检查行顺序
    print(f"\nBlock 4 first line Y: {block_4['lines'][0]['bbox'][1]:.1f}")
    print(f"Block 4 last line Y: {block_4['lines'][-1]['bbox'][3]:.1f}")
    print(f"Block 9 first line Y: {block_9['lines'][0]['bbox'][1]:.1f}")
    print(f"Block 9 last line Y: {block_9['lines'][-1]['bbox'][3]:.1f}")

    # 结论
    if y_overlap > 200:
        print(f"\n结论：Block 4 和 Block 9 的 Y 范围大量重叠（{y_overlap:.1f}），但它们仍然是独立的块")
        print("这说明 PyMuPDF 的分块策略不是基于 Y 重叠，而是基于 X 位置（列）！")


def analyze_raw_text_extraction():
    """分析 PyMuPDF 的原始文本提取"""
    doc = fitz.open("2605.23261v1.pdf")
    page = doc[0]

    # 获取更详细的文本结构
    text_page = page.get_textpage()

    # 检查每个字符的 X 坐标
    print("\n" + "="*80)
    print("检查 Block 4 中字符的 X 坐标分布：")
    print("="*80)

    block_4 = page.get_text("dict")["blocks"][4]

    all_x = []
    for line in block_4["lines"]:
        for span in line["spans"]:
            x = span["origin"][0]
            all_x.append(x)
            text = span["text"][:20]

    print(f"X coordinates in Block 4: {sorted(set(all_x))[:10]}...")
    print(f"X range: {min(all_x):.1f} - {max(all_x):.1f}")

    doc.close()


if __name__ == "__main__":
    analyze_column_grouping()
    analyze_raw_text_extraction()