package syncproto

import (
	"net/url"
	"strconv"

	"omakiten/internal/domain"
)

const (
	// ProjectsPath lists the projects the server holds.
	ProjectsPath = "/v1/projects"

	// QueryAfter and QueryLimit are the pull query parameters.
	QueryAfter = "after"
	QueryLimit = "limit"

	// DefaultPullLimit applies when a pull names no limit; MaxPullLimit caps it.
	DefaultPullLimit = 500
	MaxPullLimit     = 1000
)

// ChangesPath is the push and pull route of one project.
func ChangesPath(uid domain.UID) string {
	return ProjectsPath + "/" + string(uid) + "/changes"
}

// PullQuery encodes the pull query for a cursor and limit.
func PullQuery(after int64, limit int) string {
	return url.Values{
		QueryAfter: {strconv.FormatInt(after, 10)},
		QueryLimit: {strconv.Itoa(limit)},
	}.Encode()
}

// ProjectsResponse answers GET ProjectsPath.
type ProjectsResponse struct {
	Projects []domain.SyncProjectInfo `json:"projects"`
}

// PushRequest is the body of POST ChangesPath.
type PushRequest struct {
	Project domain.SyncProjectInfo `json:"project"`
	Changes []domain.SyncChange    `json:"changes"`
}

// PushResponse answers a push with the accepted count and the server cursor.
type PushResponse struct {
	Accepted int   `json:"accepted"`
	Rejected int   `json:"rejected"`
	Cursor   int64 `json:"cursor"`
}

// PullResponse answers GET ChangesPath; More is true when further changes follow the cursor.
type PullResponse struct {
	Project domain.SyncProjectInfo `json:"project"`
	Changes []domain.SyncChange    `json:"changes"`
	Cursor  int64                  `json:"cursor"`
	More    bool                   `json:"more"`
}
