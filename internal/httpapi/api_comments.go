package httpapi

import (
	"context"

	"net/http"
	"omakiten/internal/contract"
)

// CommentOperations reads and writes task comments.
type CommentOperations interface {
	ListComments(ctx context.Context, input contract.ListCommentsInput) (contract.CommentsResponse, error)
	AddComment(ctx context.Context, input contract.AddCommentInput) (contract.CommentResponse, error)
}

// CommentBody is the addTaskComment request body.
type CommentBody struct {
	Body         string   `json:"body"`
	Title        string   `json:"title,omitempty"`
	Kind         string   `json:"kind,omitempty"`
	Pinned       bool     `json:"pinned,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	TemplateSlug string   `json:"template_slug,omitempty"`
}

func (s *Server) commentRoutes() []route {
	return []route{
		query("listTaskComments", http.MethodGet, taskPath+"/comments", "comment.list", "Comments of a task.", []param{projectParam, taskParam}, s.listComments),
		command("addTaskComment", http.MethodPost, taskPath+"/comments", "comment.add", "Add a human comment to a task.", []param{projectParam, taskParam}, s.addComment),
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
	return ops.AddComment(r.Context(), contract.AddCommentInput{
		ProjectSelector: selector,
		TaskID:          taskID,
		Body:            body.Body,
		Title:           body.Title,
		Kind:            body.Kind,
		Pinned:          body.Pinned,
		AuthorType:      "human",
		Tags:            body.Tags,
		TemplateSlug:    body.TemplateSlug,
	})
}
