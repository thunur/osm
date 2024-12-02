package road

import (
	"github.com/natevvv/osm-ship-routing/pkg/geometry"
)

type Merger struct {
	roads           []*Segment
	mergeCount      int
	unmergableCount int
}

func NewMerger(roads []*Segment) *Merger {
	return &Merger{
		roads: roads,
	}
}

func (m *Merger) Merge() {
	// 创建节点到道路段的映射
	nodeToSegments := make(map[geometry.Point][]*Segment)

	// 构建索引
	for _, seg := range m.roads {
		if len(seg.Points) < 2 {
			m.unmergableCount++
			continue
		}

		start := seg.Points[0]
		end := seg.Points[len(seg.Points)-1]

		nodeToSegments[start] = append(nodeToSegments[start], seg)
		nodeToSegments[end] = append(nodeToSegments[end], seg)
	}

	// 合并相连的道路段
	merged := make(map[int64]bool)
	var newRoads []*Segment

	for _, seg := range m.roads {
		if merged[seg.ID] {
			continue
		}

		current := seg
		for {
			end := current.Points[len(current.Points)-1]
			connected := nodeToSegments[end]

			foundNext := false
			for _, next := range connected {
				if merged[next.ID] || next.ID == current.ID {
					continue
				}

				// 检查是否可以合并（相同道路类型、相同属性等）
				if canMerge(current, next) {
					current = mergeTwoSegments(current, next)
					merged[next.ID] = true
					m.mergeCount++
					foundNext = true
					break
				}
			}

			if !foundNext {
				break
			}
		}

		newRoads = append(newRoads, current)
	}

	m.roads = newRoads
}

func canMerge(s1, s2 *Segment) bool {
	return s1.Type == s2.Type &&
		s1.OneWay == s2.OneWay &&
		s1.MaxSpeed == s2.MaxSpeed
}

func mergeTwoSegments(s1, s2 *Segment) *Segment {
	merged := &Segment{
		ID:       s1.ID,
		Type:     s1.Type,
		OneWay:   s1.OneWay,
		MaxSpeed: s1.MaxSpeed,
		Tags:     s1.Tags,
	}

	// 合并点列表
	merged.Points = append(merged.Points, s1.Points...)
	merged.Points = append(merged.Points, s2.Points[1:]...) // 跳过第一个点，因为它与s1的最后一个点重复

	return merged
}

func (m *Merger) Roads() []*Segment {
	return m.roads
}

func (m *Merger) MergeCount() int {
	return m.mergeCount
}

func (m *Merger) UnmergableRoadCount() int {
	return m.unmergableCount
}
