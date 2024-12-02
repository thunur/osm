package pbf

import (
	"encoding/json"
	"os"
)

func ExportRoadJson(roads interface{}, importer *RoadImporter, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	return json.NewEncoder(file).Encode(roads)
}
