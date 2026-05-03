package graph

type Edge struct {
	From int64
	To   int64
}

func HasCycle(edges []Edge) bool {
	children := map[int64][]int64{}
	for _, edge := range edges {
		children[edge.From] = append(children[edge.From], edge.To)
	}

	visiting := map[int64]bool{}
	visited := map[int64]bool{}
	var visit func(int64) bool
	visit = func(node int64) bool {
		if visiting[node] {
			return true
		}
		if visited[node] {
			return false
		}
		visiting[node] = true
		for _, child := range children[node] {
			if visit(child) {
				return true
			}
		}
		visiting[node] = false
		visited[node] = true
		return false
	}

	for node := range children {
		if visit(node) {
			return true
		}
	}
	return false
}
