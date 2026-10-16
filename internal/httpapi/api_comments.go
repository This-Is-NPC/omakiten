package httpapi

import (
	"context"

	"net/http"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// CommentOperations reads and writes task and plan comments.
type CommentOperations interface {
	ListComments(ctx context.Context, input contract.ListCommentsInput) (contract.CommentsResponse, error)
	AddComment(ctx context.Context, input contract.AddCommentInput) (contract.CommentResponse, error)
	EditComment(ctx context.Context, input contract.EditCommentInput) (contract.CommentResponse, error)
	DeleteComment(ctx context.Context, input contract.DeleteCommentInput) (contract.DeleteCommentResponse, error)
}

// CommentBody is the addTaskComment and addPlanComment request body.
type CommentBody struct {
	Body         string   `json:"body"`
	Title        string   `json:"title,omitempty"`
	Kind         string   `json:"kind,omitempty"`
	Pinned       bool     `json:"pinned,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	TemplateSlug string   `json:"template_slug,omitempty"`
}

// EditCommentBody is the editComment request body; absent fields stay
// unchanged and present tags replace the stored ones.
type EditCommentBody struct {
	Body   *string  `json:"body,omitempty"`
	Title  *string  `json:"title,omitempty"`
	Kind   *string  `json:"kind,omitempty"`
	Pinned *bool    `json:"pinned,omitempty"`
	Tags   []string `json:"tags,omitempty"`
}

var commentParam = pathParam("comment", "Comment id.", idSchema)

func (s *Server) commentRoutes() []route {
	return []route{
		query("listTaskComments", http.MethodGet, taskPath+"/comments", "comment.list", "Comments of a task.", []param{projectParam, taskParam}, s.listComments),
		command("addTaskComment", http.MethodPost, taskPath+"/comments", "comment.add", "Add a human comment to a task.", []param{projectParam, taskParam}, s.addComment),
		query("listPlanComments", http.MethodGet, planPath+"/comments", "comment.list", "Comments of a plan.", []param{projectParam, planParam}, s.listPlanComments),
		command("addPlanComment", http.MethodPost, planPath+"/comments", "comment.add", "Add a human comment to a plan.", []param{projectParam, planParam}, s.addPlanComment),
		command("editComment", http.MethodPatch, projectPath+"/comments/{comment}", "comment.edit", "Edit comment fields.", []param{projectParam, commentParam}, s.editComment),
		query("deleteComment", http.MethodDelete, projectPath+"/comments/{comment}", "comment.delete", "Delete a comment; requires confirmation.", []param{projectParam, commentParam, confirmedParam}, s.deleteComment),
	}
}

func (s *Server) listComments(r *http.Request) (contract.CommentsResponse, error) {
	ops, selector, taskID, err := s.task(r)
	if err != nil {
		return contract.CommentsResponse{}, err
	}
	return ops.ListComments(r.Context(), contract.ListCommentsInput{ProjectSelector: selector, TaskID: taskID})
}

func (s *Server) addComment(r *http.Request, body CommentBody) (contract.CommentResponse, error) {
	ops, selector, taskID, err := s.task(r)
	if err != nil {
		return contract.CommentResponse{}, err
	}
	input := body.input(selector)
	input.TaskID = taskID
	return ops.AddComment(r.Context(), input)
}

func (s *Server) listPlanComments(r *http.Request) (contract.CommentsResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.CommentsResponse{}, err
	}
	return ops.ListComments(r.Context(), contract.ListCommentsInput{ProjectSelector: selector, Scope: domain.CommentScopePlan, PlanSlug: r.PathValue("plan")})
}

func (s *Server) addPlanComment(r *http.Request, body CommentBody) (contract.CommentResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.CommentResponse{}, err
	}
	input := body.input(selector)
	input.Scope = domain.CommentScopePlan
	input.PlanSlug = r.PathValue("plan")
	return ops.AddComment(r.Context(), input)
}

// input maps the body onto a human comment of the selected project.
func (b CommentBody) input(selector contract.ProjectSelector) contract.AddCommentInput {
	return contract.AddCommentInput{
		ProjectSelector: selector,
		Body:            b.Body,
		Title:           b.Title,
		Kind:            b.Kind,
		Pinned:          b.Pinned,
		AuthorType:      "human",
		Tags:            b.Tags,
		TemplateSlug:    b.TemplateSlug,
	}
}

func (s *Server) comment(r *http.Request) (Operations, contract.ProjectSelector, int64, error) {
	commentID, err := parseID(r.PathValue("comment"), "comment")
	if err != nil {
		return nil, contract.ProjectSelector{}, 0, err
	}
	ops, selector, err := s.project(r)
	return ops, selector, commentID, err
}

func (s *Server) editComment(r *http.Request, body EditCommentBody) (contract.CommentResponse, error) {
	ops, selector, commentID, err := s.comment(r)
	if err != nil {
		return contract.CommentResponse{}, err
	}
	return ops.EditComment(r.Context(), contract.EditCommentInput{
		ProjectSelector: selector,
		CommentID:       commentID,
		Body:            body.Body,
		Title:           body.Title,
		Kind:            body.Kind,
		Pinned:          body.Pinned,
		Tags:            body.Tags,
	})
}

func (s *Server) deleteComment(r *http.Request) (contract.DeleteCommentResponse, error) {
	ops, selector, commentID, err := s.comment(r)
	if err != nil {
		return contract.DeleteCommentResponse{}, err
	}
	ok, err := confirmed(r)
	if err != nil {
		return contract.DeleteCommentResponse{}, err
	}
	return ops.DeleteComment(r.Context(), contract.DeleteCommentInput{ProjectSelector: selector, CommentID: commentID, Confirmed: ok})
}
