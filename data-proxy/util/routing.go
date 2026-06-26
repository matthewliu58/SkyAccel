package util

type PathInfo struct {
	Hops   []string `json:"hops"`
	Rtt    float64  `json:"rtt"`
	RawRTT float64  `json:"raw_rtt"` // Softmax probability for load balancing
}

type RoutingInfo struct {
	Routing []PathInfo `json:"routing"`
}
