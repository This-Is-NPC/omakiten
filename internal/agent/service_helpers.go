package agent

import (
	"sort"
	"strings"
	"unicode"

	"omakiten/internal/domain"
)

// TemplateKindTask + TemplateKindComment are the canonical kind
// labels applyTemplateBody emits in validation errors and the
// service surfaces accept on the wire. Promoted from inline string
// literals so a typo at any callsite trips the compiler instead of
// silently surfacing the wrong tag in error details.
const (
	TemplateKindTask    = "task"
	TemplateKindComment = "comment"
)

// stopwordsTable builds the lowercase set wordSet drops before scoring.
// Phase 3f replaced the process-global registry with a per-Service
// field; this helper converts the per-project `config.search.stopwords`
// slice the runtime resolved into the map lookup wordSet expects.
func stopwordsTable(words []string) map[string]bool {
	if len(words) == 0 {
		return nil
	}
	set := make(map[string]bool, len(words))
	for _, w := range words {
		set[strings.ToLower(strings.TrimSpace(w))] = true
	}
	return set
}

func taskSummaries(tasks []domain.Task, registry *domain.EnumRegistry) []TaskSummary {
	out := make([]TaskSummary, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, taskSummary(task, registry))
	}
	return out
}

func dependencySummaries(dependencies []domain.TaskDependency) []DependencySummary {
	out := make([]DependencySummary, 0, len(dependencies))
	for _, dependency := range dependencies {
		out = append(out, dependencySummary(dependency))
	}
	return out
}

func commentSummaries(comments []domain.Comment) []CommentSummary {
	out := make([]CommentSummary, 0, len(comments))
	for _, comment := range comments {
		out = append(out, commentSummary(comment))
	}
	return out
}

func contextSnippets(entries []domain.ContextEntry, limit int) []ContextSnippet {
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	out := make([]ContextSnippet, 0, len(entries))
	for _, entry := range entries {
		out = append(out, contextSnippet(entry))
	}
	return out
}

func recentComments(comments []domain.Comment, limit int) []CommentSummary {
	if limit > 0 && len(comments) > limit {
		comments = comments[len(comments)-limit:]
	}
	out := commentSummaries(comments)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func findTask(tasks []domain.Task, taskID int64) (domain.Task, bool) {
	for _, task := range tasks {
		if task.ID == taskID {
			return task, true
		}
	}
	return domain.Task{}, false
}

// pendingCount returns the number of tasks NOT in the workflow's final
// (highest-position) bucket. The final lane is resolved from workflow shape
// — never compared to a hardcoded "done" — so users who rename their
// terminal bucket (e.g. "shipped", "archived") still see correct counts.
// When the workflow has no buckets the function falls back to len(tasks)
// so the surface degrades gracefully instead of returning 0.
func pendingCount(workflow domain.Workflow, tasks []domain.Task) int {
	final := workflow.FinalBucketKey()
	if final == "" {
		return len(tasks)
	}
	count := 0
	for _, task := range tasks {
		if task.BucketKey != final {
			count++
		}
	}
	return count
}

func bucketCounts(workflow domain.Workflow, tasks []domain.Task) []BucketCount {
	counts := map[string]int{}
	for _, task := range tasks {
		counts[task.BucketKey]++
	}
	out := make([]BucketCount, 0, len(workflow.Buckets))
	seen := map[string]struct{}{}
	for _, bucket := range workflow.Buckets {
		out = append(out, BucketCount{BucketKey: bucket.Key, Name: bucket.Name, Count: counts[bucket.Key]})
		seen[bucket.Key] = struct{}{}
	}
	for bucketKey, count := range counts {
		if _, ok := seen[bucketKey]; !ok {
			out = append(out, BucketCount{BucketKey: bucketKey, Count: count})
		}
	}
	return out
}

// likelyNextWork returns up to nextWorkLimit tasks that are NOT in the
// workflow's final bucket, ordered by id ASC. Same data-driven final-bucket
// resolution as pendingCount — no hardcoded "done" — so the suggestion
// list survives a bucket rename.
func likelyNextWork(workflow domain.Workflow, tasks []domain.Task, limit int, registry *domain.EnumRegistry) []TaskSummary {
	final := workflow.FinalBucketKey()
	candidates := make([]domain.Task, 0, len(tasks))
	for _, task := range tasks {
		if final == "" || task.BucketKey != final {
			candidates = append(candidates, task)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return taskSummaries(candidates, registry)
}

func blockedWork(tasks []domain.Task, dependencies []domain.TaskDependency, registry *domain.EnumRegistry) []TaskSummary {
	blocked := map[int64]struct{}{}
	for _, dependency := range dependencies {
		blocked[dependency.TaskID] = struct{}{}
	}
	out := []TaskSummary{}
	for _, task := range tasks {
		if _, ok := blocked[task.ID]; ok {
			out = append(out, taskSummary(task, registry))
		}
	}
	return out
}

// truncateBody enforces config.mcp.max_comment_chars on a comment body.
// When the body fits the budget, it is returned unchanged; when it exceeds,
// it is cut at the budget and an ellipsis is appended (`…`). The cut respects
// rune boundaries so we never split a multi-byte character. Callers should
// only invoke this when settings.MaxCommentChars > 0.
func truncateBody(body string, maxChars int) string {
	if maxChars <= 0 {
		return body
	}
	runes := []rune(body)
	if len(runes) <= maxChars {
		return body
	}
	return strings.TrimRight(string(runes[:maxChars]), " \t\r\n") + "…"
}

// applyTemplateBody resolves a template by slug from the registered catalog
// and merges its body with the user-supplied body. When the user body is
// empty, the template body wins outright; when it is non-empty, the user
// content stays first and the template body is appended after a blank line so
// the user's intent is not overwritten. The append is skipped when the user
// body already follows the template's `## ` heading structure — that signals
// the caller pre-filled the scaffold and re-appending would duplicate every
// section. Unknown slugs surface as a validation error rather than silently
// degrading. Returns the merged body, the resolved template summary, and an
// error.
//
// `kind` is just an enum tag used in error messages — use the
// TemplateKindTask / TemplateKindComment constants at callsites.
func (s *Service) applyTemplateBody(slug, body, kind string) (string, *TaskTemplateSummary, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return body, nil, nil
	}
	if s.templateCatalog == nil {
		return "", nil, domain.NewError(domain.ErrValidation, "template catalog not initialized", map[string]any{"slug": slug, "kind": kind})
	}
	for _, t := range s.templateCatalog() {
		if t.Slug != slug {
			continue
		}
		merged := mergeUserBodyWithTemplate(body, t.Body)
		summary := &TaskTemplateSummary{
			Slug:        t.Slug,
			Name:        t.Name,
			Description: t.Description,
			Body:        t.Body,
		}
		return merged, summary, nil
	}
	return "", nil, domain.NewError(domain.ErrValidation, "template not found", map[string]any{"slug": slug, "kind": kind})
}

func mergeUserBodyWithTemplate(userBody, templateBody string) string {
	user := strings.TrimSpace(userBody)
	template := strings.TrimSpace(templateBody)
	switch {
	case user == "":
		return template
	case template == "":
		return user
	}
	if userBodyFollowsTemplateStructure(user, template) {
		return user
	}
	return user + "\n\n" + template
}

// userBodyFollowsTemplateStructure detects a pre-filled scaffold so the merge
// skips the append. Heuristic: every `## ` top-level heading in templateBody
// appears verbatim as a line in userBody. Triggered by the agent flow
// `templates.show` → fill sections → `create_intent` with the same
// template_slug, which would otherwise concatenate the filled body and the
// raw scaffold and produce duplicate sections. Returns false when the
// template carries no `## ` headings — the dedupe heuristic only applies to
// section-structured templates.
func userBodyFollowsTemplateStructure(userBody, templateBody string) bool {
	headings := h2Headings(templateBody)
	if len(headings) == 0 {
		return false
	}
	userHeadings := map[string]struct{}{}
	for _, h := range h2Headings(userBody) {
		userHeadings[h] = struct{}{}
	}
	for _, h := range headings {
		if _, ok := userHeadings[h]; !ok {
			return false
		}
	}
	return true
}

func h2Headings(body string) []string {
	out := []string{}
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimRight(line, " \t\r")
		if strings.HasPrefix(trimmed, "## ") {
			out = append(out, trimmed)
		}
	}
	return out
}

func taskTitleAndDescription(title, description string) (string, string) {
	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	if title != "" {
		return title, description
	}
	if description == "" {
		return "", ""
	}
	line := strings.TrimSpace(strings.Split(description, "\n")[0])
	if len(line) > 90 {
		line = strings.TrimSpace(line[:90])
	}
	return line, description
}

func similarTasks(query string, tasks []domain.Task, limit int, registry *domain.EnumRegistry, stops map[string]bool) []TaskSummary {
	queryWords := wordSet(query, stops)
	if len(queryWords) == 0 {
		return nil
	}
	type match struct {
		task  domain.Task
		score float64
	}
	matches := []match{}
	queryLower := strings.ToLower(strings.TrimSpace(query))
	for _, task := range tasks {
		text := task.Title + " " + task.Description
		textLower := strings.ToLower(strings.TrimSpace(text))
		words := wordSet(text, stops)
		score := overlapScore(queryWords, words)
		if textLower == queryLower || strings.Contains(textLower, queryLower) || strings.Contains(queryLower, strings.ToLower(task.Title)) {
			score = 1
		}
		if score >= 0.5 {
			matches = append(matches, match{task: task, score: score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return matches[i].task.ID < matches[j].task.ID
		}
		return matches[i].score > matches[j].score
	})
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	out := make([]TaskSummary, 0, len(matches))
	for _, match := range matches {
		out = append(out, taskSummary(match.task, registry))
	}
	return out
}

func wordSet(value string, stops map[string]bool) map[string]struct{} {
	words := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	out := map[string]struct{}{}
	for _, word := range words {
		if len(word) < 3 {
			continue
		}
		if stops != nil && stops[word] {
			continue
		}
		out[word] = struct{}{}
	}
	return out
}

func overlapScore(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	common := 0
	for word := range a {
		if _, ok := b[word]; ok {
			common++
		}
	}
	return float64(common) / float64(len(a))
}

