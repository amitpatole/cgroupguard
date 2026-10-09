package detect

func classify(v1, v2 bool) string {
	if v1 && v2 {
		return "hybrid"
	}
	if v1 {
		return "v1"
	}
	return "unknown"
}
