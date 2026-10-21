package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCLIDocsListsAndShowsShippedGuides(t *testing.T) {
	db := filepath.Join(t.TempDir(), "omakiten.db")
	config := filepath.Join(t.TempDir(), "config.yaml")

	var list struct {
		Data struct {
			Guides []struct {
				Topic    string `json:"topic"`
				Title    string `json:"title"`
				Markdown string `json:"markdown"`
			} `json:"guides"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(runCLI(t, db, config, "docs", "list")), &list); err != nil {
		t.Fatalf("docs list output: %v", err)
	}
	found := false
	for _, guide := range list.Data.Guides {
		if guide.Markdown != "" {
			t.Fatalf("docs list carries the body of %s", guide.Topic)
		}
		found = found || guide.Topic == "how-to-use-hooks" && guide.Title == "How to run an action when work changes"
	}
	if !found {
		t.Fatalf("docs list = %+v, want how-to-use-hooks", list.Data.Guides)
	}

	var show struct {
		Data struct {
			Topic    string `json:"topic"`
			Markdown string `json:"markdown"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(runCLI(t, db, config, "docs", "show", "how-to-use-hooks")), &show); err != nil {
		t.Fatalf("docs show output: %v", err)
	}
	if show.Data.Topic != "how-to-use-hooks" || show.Data.Markdown == "" {
		t.Fatalf("docs show = %+v", show.Data)
	}

	runCLIExpectError(t, db, config, "validation_error", "docs", "show", "no-such-guide")
	if _, err := os.Stat(db); !os.IsNotExist(err) {
		t.Fatalf("docs created or required a database: %v", err)
	}
}
