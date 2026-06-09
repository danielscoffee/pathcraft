package osm

type Node struct {
	ID   int64
	Lat  float64
	Lon  float64
	Tags map[string]string
}

type Way struct {
	ID      int64
	NodeIDs []int64
	Tags    map[string]string
}

type Data struct {
	Nodes map[int64]*Node
	Ways  []*Way
}

func NewData() *Data {
	return &Data{
		Nodes: make(map[int64]*Node),
		Ways:  make([]*Way, 0),
	}
}

type xmlOSM struct {
	Nodes []xmlNode `xml:"node"`
	Ways  []xmlWay  `xml:"way"`
}

type xmlNode struct {
	ID   int64    `xml:"id,attr"`
	Lat  float64  `xml:"lat,attr"`
	Lon  float64  `xml:"lon,attr"`
	Tags []xmlTag `xml:"tag"`
}

type xmlWay struct {
	ID       int64    `xml:"id,attr"`
	NodeRefs []xmlNd  `xml:"nd"`
	Tags     []xmlTag `xml:"tag"`
}

type xmlNd struct {
	Ref int64 `xml:"ref,attr"`
}

type xmlTag struct {
	K string `xml:"k,attr"`
	V string `xml:"v,attr"`
}
