package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/natevvv/osm-ship-routing/internal/pbf"
	"github.com/natevvv/osm-ship-routing/pkg/road"
)

var flagPbfFile = flag.String("f", "china.osm.pbf", "PBF文件路径")
var flagOutputFile = flag.String("o", "china.road.json", "输出的道路网络文件路径")

func main() {
	flag.Parse()

	start := time.Now()

	roadImporter := pbf.NewRoadImporter(*flagPbfFile)
	roadImporter.Import()

	elapsed := time.Since(start)
	fmt.Printf("[TIME] 导入用时: %s\n", elapsed)

	start = time.Now()

	merger := road.NewMerger(roadImporter.Roads())
	merger.Merge()

	elapsed = time.Since(start)
	fmt.Printf("[TIME] 合并用时: %s\n", elapsed)
	fmt.Printf("道路段数量: %d\n", len(merger.Roads()))
	fmt.Printf("合并次数: %d\n", merger.MergeCount())
	fmt.Printf("未能合并的道路段: %d\n", merger.UnmergableRoadCount())

	start = time.Now()

	pbf.ExportRoadJson(merger.Roads(), roadImporter, *flagOutputFile)

	elapsed = time.Since(start)
	fmt.Printf("[TIME] 导出用时: %s\n", elapsed)
	fmt.Printf("已导出道路网络到 %s\n", *flagOutputFile)
}
