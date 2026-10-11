package daemon

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestDaemonProjectRelocationPreservesIdentityAndRejectsCollision(t *testing.T) {
	f := newDaemonFixture(t)
	rt := f.open(t)
	project := registerProject(t, rt, "alpha", f.root)
	other := registerProject(t, rt, "other", filepath.Join(filepath.Dir(f.root), "other"))
	discovery, token, _ := startDaemon(t, rt)
	url := discovery.URL + "/api/v1/projects/alpha"
	status, body := request(t, http.MethodGet, url, token, "")
	if status != http.StatusOK {
		t.Fatalf("overview: %d %+v", status, body)
	}
	root := filepath.Join(filepath.Dir(f.root), "moved")
	if err := os.Rename(f.root, root); err != nil {
		t.Fatal(err)
	}
	status, body = request(t, http.MethodPost, url+"/relocate", token, fmt.Sprintf(`{"root_path":%q}`, root))
	if status != http.StatusOK {
		t.Fatalf("relocate: %d %+v", status, body)
	}
	data := body["data"].(map[string]any)
	got := data["project"].(map[string]any)
	if got["id"] != float64(project.ID) || got["root_path"] != root || data["previous_root_path"] != f.root {
		t.Fatalf("identity changed: %+v", data)
	}
	status, body = request(t, http.MethodGet, url, token, "")
	if status != http.StatusOK || body["data"].(map[string]any)["project"].(map[string]any)["root_path"] != root {
		t.Fatalf("stale overview: %d %+v", status, body)
	}
	status, body = request(t, http.MethodPost, url+"/relocate", token, fmt.Sprintf(`{"root_path":%q}`, other.RootPath))
	if status != http.StatusConflict || body["code"] != "project_path_conflict" {
		t.Fatalf("collision: %d %+v", status, body)
	}
	status, body = request(t, http.MethodPost, url+"/relocate", token, `{"root_path":"relative"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("relative root: %d %+v", status, body)
	}
}

func TestDaemonRelocationFollowsMovedSessionConfiguration(t *testing.T) {
	f := newDaemonFixture(t)
	f.configPath = filepath.Join(f.root, ".omakiten", "config", "omakase.yaml")
	rt := f.open(t)
	project := registerProject(t, rt, "alpha", f.root)
	writeRepoLocalConfig(t, f.root, "preset: config/omakase.yaml\n")
	s := session(rt)
	s.Project = project.Context()
	s.RepoLocalDir = filepath.Join(f.root, ".omakiten")
	discovery, token, _ := startSession(t, s)
	root := filepath.Join(filepath.Dir(f.root), "moved")
	if err := os.Rename(f.root, root); err != nil {
		t.Fatal(err)
	}
	url := discovery.URL + "/api/v1/projects/alpha"
	status, body := request(t, http.MethodPost, url+"/relocate", token, fmt.Sprintf(`{"root_path":%q}`, root))
	if status != http.StatusOK {
		t.Fatalf("relocate moved local config: %d %+v", status, body)
	}
	status, body = request(t, http.MethodGet, url, token, "")
	if status != http.StatusOK {
		t.Fatalf("load moved local config: %d %+v", status, body)
	}
	if rt.Cache().Get(s.CacheProjectID).SourcePath != filepath.Join(root, ".omakiten", "config.yaml") {
		t.Fatal("runtime kept old configuration path")
	}
	if rt.Cache().Size() != 1 {
		t.Fatal("relocation duplicated the session runtime")
	}
	status, body = request(t, http.MethodGet, discovery.URL+"/api/v1/projects", token, "")
	if status != http.StatusOK {
		t.Fatalf("global call after session relocation: %d %+v", status, body)
	}
	if rt.Cache().Get(s.CacheProjectID).SourcePath != filepath.Join(root, ".omakiten", "config.yaml") {
		t.Fatal("global call reverted the runtime path")
	}
}
