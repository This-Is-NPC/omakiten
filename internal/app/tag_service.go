package app

import (
	"context"
	"encoding/json"
	"strings"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

const (
	TagEntityTask    = "task"
	TagEntityProject = "project"
	TagEntityError   = "error"
)

type TagService struct {
	repo     TagRepository
	events   EventRepository
	synonyms map[string]string
}

func NewTagService(repo TagRepository) *TagService {
	return &TagService{repo: repo}
}

// NewTagServiceWithEvents wires emission of tag.added / tag.removed
// alongside the canonical write. events may be nil (callers that do not
// want emission keep using NewTagService); telemetry errors are swallowed
// so they cannot break business logic.
func NewTagServiceWithEvents(repo TagRepository, events EventRepository) *TagService {
	return &TagService{repo: repo, events: events}
}

// SetSynonyms installs the per-project tag-synonym table the service
// passes to NormalizeTagName on every Add/Remove. Phase 3f replaced
// the previous process-global registry with this per-service field so
// two projects' synonym tables stay disjoint in the same binary.
func (s *TagService) SetSynonyms(synonyms map[string]string) {
	s.synonyms = synonyms
}

func (s *TagService) emitTagEvent(ctx context.Context, eventType, entityType string, entityID, projectID, tagID int64, tagName string) {
	if s.events == nil {
		return
	}
	body, err := json.Marshal(map[string]any{
		"entity_type": entityType,
		"entity_id":   entityID,
		"tag_id":      tagID,
		"tag_name":    tagName,
	})
	if err != nil {
		return
	}
	rowEntity := domain.EventEntityTask
	rowEntityID := entityID
	switch entityType {
	case TagEntityProject:
		rowEntity = domain.EventEntityProject
		rowEntityID = projectID
	case TagEntityError:
		rowEntity = domain.EventEntityError
	}
	_ = s.events.RecordEntityEvent(ctx, rowEntity, rowEntityID, projectID, eventType, string(body))
}

func (s *TagService) Add(ctx context.Context, project domain.ProjectContext, entityType string, entityID int64, tagName string) (tag domain.Tag, err error) {
	finish := activity.Track(ctx, "app.TagService.Add", project, map[string]any{"entity_type": entityType, "entity_id": entityID, "tag": tagName})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	tagName = strings.TrimSpace(tagName)
	if tagName == "" {
		err = domain.NewError(domain.ErrValidation, "tag name is required", nil)
		return
	}
	normalized := NormalizeTagName(tagName, s.synonyms)
	if normalized == "" {
		err = domain.NewError(domain.ErrValidation, "tag name is invalid after normalization", map[string]any{"input": tagName})
		return
	}

	tag, err = s.repo.FindOrCreateTag(ctx, normalized, TagLabel(tagName))
	if err != nil {
		return
	}

	switch entityType {
	case TagEntityTask:
		if entityID <= 0 {
			err = domain.NewError(domain.ErrValidation, "entity_id must be positive for task tags", nil)
			return
		}
		err = s.repo.AddTaskTag(ctx, project.ID, entityID, tag.ID)
	case TagEntityProject:
		err = s.repo.AddProjectTag(ctx, project.ID, tag.ID)
	case TagEntityError:
		if entityID <= 0 {
			err = domain.NewError(domain.ErrValidation, "entity_id must be positive for error tags", nil)
			return
		}
		err = s.repo.AddErrorTag(ctx, entityID, tag.ID)
	default:
		err = domain.NewError(domain.ErrValidation, "entity_type must be 'task', 'project', or 'error'", map[string]any{"entity_type": entityType})
	}
	if err == nil {
		s.emitTagEvent(ctx, domain.EventTypeTagAdded, entityType, entityID, project.ID, tag.ID, tag.Name)
	}
	return
}

func (s *TagService) Remove(ctx context.Context, project domain.ProjectContext, entityType string, entityID, tagID int64) (err error) {
	finish := activity.Track(ctx, "app.TagService.Remove", project, map[string]any{"entity_type": entityType, "entity_id": entityID, "tag_id": tagID})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	if tagID <= 0 {
		err = domain.NewError(domain.ErrValidation, "tag_id must be positive", nil)
		return
	}

	switch entityType {
	case TagEntityTask:
		if entityID <= 0 {
			err = domain.NewError(domain.ErrValidation, "entity_id must be positive for task tags", nil)
			return
		}
		err = s.repo.RemoveTaskTag(ctx, project.ID, entityID, tagID)
	case TagEntityProject:
		err = s.repo.RemoveProjectTag(ctx, project.ID, tagID)
	case TagEntityError:
		if entityID <= 0 {
			err = domain.NewError(domain.ErrValidation, "entity_id must be positive for error tags", nil)
			return
		}
		err = s.repo.RemoveErrorTag(ctx, entityID, tagID)
	default:
		err = domain.NewError(domain.ErrValidation, "entity_type must be 'task', 'project', or 'error'", map[string]any{"entity_type": entityType})
	}
	if err == nil {
		s.emitTagEvent(ctx, domain.EventTypeTagRemoved, entityType, entityID, project.ID, tagID, "")
	}
	return
}

func (s *TagService) List(ctx context.Context, project domain.ProjectContext, entityType string, entityID int64) (tags []domain.Tag, err error) {
	finish := activity.Track(ctx, "app.TagService.List", project, map[string]any{"entity_type": entityType, "entity_id": entityID})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	switch entityType {
	case TagEntityTask:
		if entityID <= 0 {
			err = domain.NewError(domain.ErrValidation, "entity_id must be positive for task tags", nil)
			return
		}
		tags, err = s.repo.ListTaskTags(ctx, project.ID, entityID)
	case TagEntityProject:
		tags, err = s.repo.ListProjectTags(ctx, project.ID)
	case TagEntityError:
		if entityID <= 0 {
			err = domain.NewError(domain.ErrValidation, "entity_id must be positive for error tags", nil)
			return
		}
		tags, err = s.repo.ListErrorTags(ctx, entityID)
	default:
		err = domain.NewError(domain.ErrValidation, "entity_type must be 'task', 'project', or 'error'", map[string]any{"entity_type": entityType})
	}
	return
}

func (s *TagService) ListAll(ctx context.Context) (tags []domain.Tag, err error) {
	return s.repo.ListAllTags(ctx)
}

func (s *TagService) Merge(ctx context.Context, sourceTagID, targetTagID int64) (tag domain.Tag, err error) {
	if sourceTagID <= 0 || targetTagID <= 0 {
		return domain.Tag{}, domain.NewError(domain.ErrValidation, "tag ids must be positive", nil)
	}
	if sourceTagID == targetTagID {
		return domain.Tag{}, domain.NewError(domain.ErrValidation, "source and target tags must be different", nil)
	}
	return s.repo.MergeTags(ctx, sourceTagID, targetTagID)
}

func (s *TagService) DeleteOrphans(ctx context.Context) (int64, error) {
	return s.repo.DeleteOrphanTags(ctx)
}

func (s *TagService) Rename(ctx context.Context, tagID int64, newLabel string) (domain.Tag, error) {
	newLabel = strings.TrimSpace(newLabel)
	if newLabel == "" {
		return domain.Tag{}, domain.NewError(domain.ErrValidation, "label is required", nil)
	}
	if tagID <= 0 {
		return domain.Tag{}, domain.NewError(domain.ErrValidation, "tag_id must be positive", nil)
	}
	return s.repo.RenameTag(ctx, tagID, newLabel)
}
