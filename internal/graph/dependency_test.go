package graph

import "testing"

func TestHasCycle(t *testing.T) {
	tests := []struct {
		name  string
		edges []Edge
		want  bool
	}{
		{
			name:  "empty graph",
			edges: nil,
			want:  false,
		},
		{
			name:  "linear chain",
			edges: []Edge{{From: 1, To: 2}, {From: 2, To: 3}, {From: 3, To: 4}},
			want:  false,
		},
		{
			name:  "diamond",
			edges: []Edge{{From: 1, To: 2}, {From: 1, To: 3}, {From: 2, To: 4}, {From: 3, To: 4}},
			want:  false,
		},
		{
			name:  "self loop",
			edges: []Edge{{From: 1, To: 1}},
			want:  true,
		},
		{
			name:  "two node cycle",
			edges: []Edge{{From: 1, To: 2}, {From: 2, To: 1}},
			want:  true,
		},
		{
			name:  "transitive cycle",
			edges: []Edge{{From: 1, To: 2}, {From: 2, To: 3}, {From: 3, To: 1}},
			want:  true,
		},
		{
			name:  "cycle in one disconnected component",
			edges: []Edge{{From: 10, To: 11}, {From: 1, To: 2}, {From: 2, To: 3}, {From: 3, To: 1}},
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasCycle(tt.edges); got != tt.want {
				t.Fatalf("HasCycle(%v) = %v, want %v", tt.edges, got, tt.want)
			}
		})
	}
}
