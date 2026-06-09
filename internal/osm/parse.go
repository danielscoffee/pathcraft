package osm

import (
	"compress/gzip"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"
)

func ParseXML(r io.Reader) (*Data, error) {
	var osm xmlOSM
	decoder := xml.NewDecoder(r)
	if err := decoder.Decode(&osm); err != nil {
		return nil, fmt.Errorf("decoding XML: %w", err)
	}

	data := NewData()

	for _, n := range osm.Nodes {
		tags := make(map[string]string)
		for _, t := range n.Tags {
			tags[t.K] = t.V
		}
		data.Nodes[n.ID] = &Node{
			ID:   n.ID,
			Lat:  n.Lat,
			Lon:  n.Lon,
			Tags: tags,
		}
	}

	for _, w := range osm.Ways {
		tags := make(map[string]string)
		for _, t := range w.Tags {
			tags[t.K] = t.V
		}

		nodeIDs := make([]int64, len(w.NodeRefs))
		for i, nd := range w.NodeRefs {
			nodeIDs[i] = nd.Ref
		}

		data.Ways = append(data.Ways, &Way{
			ID:      w.ID,
			NodeIDs: nodeIDs,
			Tags:    tags,
		})
	}

	return data, nil
}

func ParseFile(path string) (*Data, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = f.Close()
	}()

	var reader io.Reader = f

	if strings.HasSuffix(path, ".gz") {
		gzReader, err := gzip.NewReader(f)
		if err != nil {
			return nil, fmt.Errorf("opening gzip: %w", err)
		}
		defer func() {
			_ = gzReader.Close()
		}()
		reader = gzReader
	}

	return ParseXML(reader)
}
