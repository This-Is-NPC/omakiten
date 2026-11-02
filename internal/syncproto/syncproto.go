package syncproto

import (
	"net/url"
	"slices"
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
	Cursor   int64 `json:"cursor"`
}

// PullResponse answers GET ChangesPath; More is true when further changes follow the cursor.
type PullResponse struct {
	Project domain.SyncProjectInfo `json:"project"`
	Changes []domain.SyncChange    `json:"changes"`
	Cursor  int64                  `json:"cursor"`
	More    bool                   `json:"more"`
}

// ValidateChange refuses a change the server or a client must not store.
func ValidateChange(c domain.SyncChange) error {
	if !slices.Contains(domain.SyncTables, c.Table) {
		return invalid("table", c.Table, "is not a synced table")
	}
	if c.Op != domain.SyncOpUpsert && c.Op != domain.SyncOpDelete {
		return invalid("op", string(c.Op), "must be upsert or delete")
	}
	if c.RowUID == "" {
		return invalid("row_uid", "", "must not be empty")
	}
	hlc, err := domain.ParseHLC(c.HLC)
	if err != nil {
		return err
	}
	if hlc == (domain.HLC{}) {
		return invalid("hlc", c.HLC, "must not be zero")
	}
	if _, err := domain.ParseUID(string(c.Device)); err != nil {
		return err
	}
	if c.Op == domain.SyncOpUpsert && len(c.Data) == 0 {
		return invalid("data", "", "an upsert needs data")
	}
	return nil
}

func invalid(field, value, reason string) error {
	return domain.NewError(domain.ErrValidation,
		"invalid change "+field+" "+strconv.Quote(value)+": "+reason,
		map[string]any{"field": field, "value": value})
}
