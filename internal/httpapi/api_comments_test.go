package httpapi

import (
	"context"
	"net/http"

	"omakiten/internal/contract"
)

var _ = mapping([]mappingCase{
	{"listTaskComments", http.MethodGet, "/api/v1/projects/alpha/tasks/5/comments", "", "ListComments", contract.ListCommentsInput{ProjectSelector: alpha, TaskID: 5}},
	{"addTaskComment", http.MethodPost, "/api/v1/projects/alpha/tasks/5/comments", `{"body":"B","title":"Ti","kind":"note","pinned":true,"tags":["a"],"template_slug":"tpl"}`, "AddComment", contract.AddCommentInput{
		ProjectSelector: alpha, TaskID: 5, Body: "B", Title: "Ti", Kind: "note", Pinned: true, AuthorType: "human", Tags: []string{"a"}, TemplateSlug: "tpl",
	}},
})

func (f *fakeOps) ListComments(_ context.Context, in contract.ListCommentsInput) (contract.CommentsResponse, error) {
	f.record("ListComments", in)
	return contract.CommentsResponse{}, nil
}

func (f *fakeOps) AddComment(_ context.Context, in contract.AddCommentInput) (contract.CommentResponse, error) {
	f.record("AddComment", in)
	return contract.CommentResponse{}, nil
}
