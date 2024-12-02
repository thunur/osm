package graph

type Arc struct {
	To       int
	Distance int
	RoadType string
}

func (a Arc) ArcFlag() bool {
	switch a.RoadType {
	case "motorway", "trunk", "primary", "secondary", "tertiary":
		return true
	default:
		return false
	}
}

func (a *Arc) SetArcFlag(flag bool) {
	if flag {
		a.RoadType = "primary"
	} else {
		a.RoadType = "unclassified"
	}
}
