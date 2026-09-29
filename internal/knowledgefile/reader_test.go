package knowledgefile

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/domain"
)

func TestLoadLocalAndDeclaredRelatedKnowledge(t *testing.T) {
	root := t.TempDir()
	api := filepath.Join(root, "api")
	client := filepath.Join(root, "client")
	writeKnowledgeTestFile(t, filepath.Join(api, "docs", "knowledge.yaml"), "version: 1\nsources:\n  - {format: openapi, path: api.yaml}\n")
	writeKnowledgeTestFile(t, filepath.Join(api, "api.yaml"), "openapi: 3.1.0\ninfo: {title: API, version: 1.0.0}\npaths:\n  /orders:\n    post:\n      operationId: createOrder\n      summary: Create an order\n      description: Creates an order for the client.\n      responses:\n        '201':\n          description: Created\n          content:\n            application/json:\n              schema: {'$ref': '#/components/schemas/Order'}\ncomponents:\n  schemas:\n    Order: {type: object, description: An order}\n")
	writeKnowledgeTestFile(t, filepath.Join(client, "docs", "knowledge.yaml"), "version: 1\nsources:\n  - {format: okf, path: docs}\nrelated_projects: [api]\n")
	writeKnowledgeTestFile(t, filepath.Join(client, "docs", "checkout.md"), "---\ntype: Reference\ntitle: Checkout\n---\nUse [createOrder](okt://api/openapi:createOrder).\n")
	project := domain.ProjectContext{Slug: "client", RootPath: client}
	local := Load(context.Background(), project, false, nil)
	if len(local.Diagnostics) != 0 || len(local.Resources) != 1 || len(local.Relations) != 1 {
		t.Fatalf("local knowledge = %+v", local)
	}
	all := Load(context.Background(), project, true, func(_ context.Context, slug string) (domain.Project, error) {
		if slug != "api" {
			t.Fatalf("unexpected related project %s", slug)
		}
		return domain.Project{Slug: "api", RootPath: api}, nil
	})
	if len(all.Diagnostics) != 0 || len(all.Resources) != 3 || len(all.Relations) != 2 {
		t.Fatalf("combined knowledge = %+v", all)
	}
	if all.Relations[0].To != "api:openapi:createOrder" && all.Relations[1].To != "api:openapi:createOrder" {
		t.Fatalf("cross-project operation missing: %+v", all.Relations)
	}
	if !strings.Contains(all.Resources[0].Body, "POST /orders") {
		t.Fatalf("OpenAPI operation body missing: %+v", all.Resources)
	}
}

func TestLoadRejectsPathEscapeAndSymlink(t *testing.T) {
	root := t.TempDir()
	writeKnowledgeTestFile(t, filepath.Join(root, "docs", "knowledge.yaml"), "version: 1\nsources:\n  - {format: markdown, path: ../secret.md}\n")
	snapshot := Load(context.Background(), domain.ProjectContext{Slug: "test", RootPath: root}, false, nil)
	if len(snapshot.Diagnostics) != 1 || !strings.Contains(snapshot.Diagnostics[0], "escapes project root") {
		t.Fatalf("path escape diagnostics = %+v", snapshot.Diagnostics)
	}
	writeKnowledgeTestFile(t, filepath.Join(root, "docs", "knowledge.yaml"), "version: 1\nsources:\n  - {format: markdown, path: docs/link.md}\n")
	writeKnowledgeTestFile(t, filepath.Join(root, "target.md"), "# Target\n")
	if err := os.Symlink(filepath.Join(root, "target.md"), filepath.Join(root, "docs", "link.md")); err != nil {
		t.Fatal(err)
	}
	snapshot = Load(context.Background(), domain.ProjectContext{Slug: "test", RootPath: root}, false, nil)
	if len(snapshot.Diagnostics) != 1 || !strings.Contains(snapshot.Diagnostics[0], "symlink") {
		t.Fatalf("symlink diagnostics = %+v", snapshot.Diagnostics)
	}
}

func TestLoadDefaultMarkdownFromDocs(t *testing.T) {
	root := t.TempDir()
	writeKnowledgeTestFile(t, filepath.Join(root, "docs", "index.md"), "# Start\n\nSee [Guide](guide.md).\n")
	writeKnowledgeTestFile(t, filepath.Join(root, "docs", "guide.md"), "# Guide\n\nSteps.\n")
	snapshot := Load(context.Background(), domain.ProjectContext{Slug: "test", RootPath: root}, false, nil)
	if len(snapshot.Diagnostics) != 0 || len(snapshot.Resources) != 2 || len(snapshot.Relations) != 1 || snapshot.Relations[0].To != "test:markdown:docs/guide" {
		t.Fatalf("default docs = %+v", snapshot)
	}
}

func TestLoadCLIDocumentLinksToOpenAPI(t *testing.T) {
	root := t.TempDir()
	writeKnowledgeTestFile(t, filepath.Join(root, "docs", "knowledge.yaml"), "version: 1\nsources:\n  - {format: cli, path: docs/commands.yaml}\n  - {format: openapi, path: openapi.yaml}\n")
	writeKnowledgeTestFile(t, filepath.Join(root, "docs", "commands.yaml"), "version: 1\ncommands:\n  - id: orders-create\n    name: shop orders create\n    summary: Create an order\n    links: [openapi:createOrder]\n")
	writeKnowledgeTestFile(t, filepath.Join(root, "openapi.yaml"), "openapi: 3.1.0\ninfo: {title: Shop, version: 1.0.0}\npaths:\n  /orders:\n    post: {operationId: createOrder, summary: Create an order}\n")
	snapshot := Load(context.Background(), domain.ProjectContext{Slug: "shop", RootPath: root}, false, nil)
	if len(snapshot.Diagnostics) != 0 || len(snapshot.Resources) != 2 || len(snapshot.Relations) != 1 || snapshot.Relations[0].To != "shop:openapi:createOrder" {
		t.Fatalf("CLI knowledge = %+v", snapshot)
	}
}

func writeKnowledgeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
