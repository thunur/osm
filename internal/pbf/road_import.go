package pbf

import (
	"os"
	"runtime"
	"sync"

	"github.com/natevvv/osm-ship-routing/pkg/geometry"
	"github.com/natevvv/osm-ship-routing/pkg/road"
	"github.com/qedus/osmpbf"
)

type RoadImporter struct {
	filename string
	roads    []*road.Segment
	nodes    map[int64]geometry.Point
}

func NewRoadImporter(filename string) *RoadImporter {
	return &RoadImporter{
		filename: filename,
		roads:    make([]*road.Segment, 0),
		nodes:    make(map[int64]geometry.Point),
	}
}

func (ri *RoadImporter) Import() error {
	if err := ri.collectNodes(); err != nil {
		return err
	}

	file, err := os.Open(ri.filename)
	if err != nil {
		return err
	}
	defer file.Close()

	decoder := osmpbf.NewDecoder(file)
	decoder.SetBufferSize(osmpbf.MaxBlobSize)

	err = decoder.Start(runtime.GOMAXPROCS(-1))
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	roadsChan := make(chan *road.Segment, 1000)

	// 启动处理协程
	wg.Add(1)
	go func() {
		defer wg.Done()
		for segment := range roadsChan {
			ri.roads = append(ri.roads, segment)
		}
	}()

	for {
		if v, err := decoder.Decode(); err == nil {
			switch v := v.(type) {
			case *osmpbf.Way:
				if highway, ok := v.Tags["highway"]; ok {
					// 只处理主要道路类型
					roadType := getRoadType(highway)
					if roadType != road.Unknown {
						segment := &road.Segment{
							ID:     v.ID,
							Type:   roadType,
							Tags:   v.Tags,
							OneWay: v.Tags["oneway"] == "yes",
							Points: make([]geometry.Point, 0, len(v.NodeIDs)),
						}
						// 添加节点坐标
						for _, nodeID := range v.NodeIDs {
							if point, ok := ri.nodes[nodeID]; ok {
								segment.Points = append(segment.Points, point)
							}
						}
						if len(segment.Points) > 0 {
							roadsChan <- segment
						}
					}
				}
			}
		} else {
			close(roadsChan)
			break
		}
	}

	wg.Wait()
	return nil
}

func getRoadType(highway string) road.RoadType {
	switch highway {
	case "motorway":
		return road.Motorway
	case "trunk":
		return road.Trunk
	case "primary":
		return road.Primary
	case "secondary":
		return road.Secondary
	case "tertiary":
		return road.Tertiary
	default:
		return road.Unknown
	}
}

func (ri *RoadImporter) Roads() []*road.Segment {
	return ri.roads
}

func (ri *RoadImporter) collectNodes() error {
	file, err := os.Open(ri.filename)
	if err != nil {
		return err
	}
	defer file.Close()

	decoder := osmpbf.NewDecoder(file)
	decoder.SetBufferSize(osmpbf.MaxBlobSize)

	err = decoder.Start(runtime.GOMAXPROCS(-1))
	if err != nil {
		return err
	}

	for {
		if v, err := decoder.Decode(); err == nil {
			switch v := v.(type) {
			case *osmpbf.Node:
				ri.nodes[v.ID] = geometry.MakePoint(v.Lat, v.Lon)
			}
		} else {
			break
		}
	}
	return nil
}
