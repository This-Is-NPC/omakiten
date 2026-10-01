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
	{"editComment", http.MethodPatch, "/api/v1/projects/alpha/comments/9", `{"title":"Ti","pinned":false,"tags":[]}`, "EditComment", contract.EditCommentInput{
		ProjectSelector: alpha, CommentID: 9, Title: ptr("Ti"), Pinned: ptr(false), Tags: []string{},
	}},
	{"deleteComment", http.MethodDelete, "/api/v1/projects/alpha/comments/9?confirmed=true", "", "DeleteComment", contract.DeleteCommentInput{ProjectSelector: alpha, CommentID: 9, Confirmed: true}},
})

func (f *fakeOps) ListComments(_ context.Context, in contract.ListCommentsInput) (contract.CommentsResponse, error) {
	f.record("ListComments", in)
	return contract.CommentsResponse{}, nil
}

func (f *fakeOps) AddComment(_ context.Context, in contract.AddCommentInput) (contract.CommentResponse, error) {
	f.record("AddComment", in)
	return contract.CommentResponse{}, nil
}

func (f *fakeOps) EditComment(_ context.Context, in contract.EditCommentInput) (contract.CommentResponse, error) {
	f.record("EditComment", in)
	return contract.CommentResponse{}, nil
}

func (f *fakeOps) DeleteComment(_ context.Context, in contract.DeleteCommentInput) (contract.DeleteCommentResponse, error) {
	f.record("DeleteComment", in)
	return contract.DeleteCommentResponse{}, nil
}
