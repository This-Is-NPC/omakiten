package domain

type Workflow struct {
	ID      int64    `json:"id"`
	Key     string   `json:"key"`
	Name    string   `json:"name"`
	Buckets []Bucket `json:"buckets,omitempty"`
}

type Bucket struct {
	ID       int64  `json:"id"`
	Key      string `json:"key"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}
