package agentruntime

import (
	"context"

	"omakiten/internal/contract"
)

func (r *ProjectRuntime) view() *contract.RuntimeView {
	if r == nil {
		return nil
	}
	var service contract.Operations
	if r.Service != nil {
		service = r.Service.ForTUI()
	}
	r.deliveryService = r.Service
	return &contract.RuntimeView{Service: service, Snapshot: r.Snapshot, PreviousSnapshot: r.PreviousSnapshot, EnumRegistry: r.EnumRegistry, Editor: r.Editor, SourcePath: r.SourcePath}
}

// View projects a cache entry onto delivery contracts without exposing runtime resources.
func (c *BundleCache) View(projectID int64) *contract.RuntimeView {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.entries[projectID]
	if r == nil {
		return nil
	}
	if r.deliveryView == nil || r.deliveryService != r.Service || r.deliveryView.Snapshot != r.Snapshot || r.deliveryView.PreviousSnapshot != r.PreviousSnapshot || r.deliveryView.Editor != r.Editor {
		r.deliveryView = r.view()
	}
	return r.deliveryView
}

func (c *BundleCache) ResolveView(ctx context.Context, id int64, path string) (*contract.RuntimeView, error) {
	if _, err := c.Resolve(ctx, id, path); err != nil {
		return nil, err
	}
	return c.View(id), nil
}
func (c *BundleCache) ReloadView(ctx context.Context, id int64, path string) (*contract.RuntimeView, error) {
	if _, err := c.Reload(ctx, id, path); err != nil {
		return nil, err
	}
	return c.View(id), nil
}
func (c *BundleCache) ApplyView(ctx context.Context, id int64, path string, accept func(*contract.RuntimeView) (func() error, error)) (*contract.RuntimeView, error) {
	_, err := c.ApplyWithCommit(ctx, id, path, func(r *ProjectRuntime) (func() error, error) {
		r.deliveryView = r.view()
		return accept(r.deliveryView)
	})
	if err != nil {
		return nil, err
	}
	return c.View(id), nil
}
func (c *BundleCache) ResolveApplyView(ctx context.Context, id int64, path string, accept func(*contract.RuntimeView) (func() error, error)) (*contract.RuntimeView, bool, error) {
	_, changed, err := c.ResolveApplyWithCommit(ctx, id, path, func(r *ProjectRuntime) (func() error, error) {
		r.deliveryView = r.view()
		return accept(r.deliveryView)
	})
	if err != nil {
		return nil, false, err
	}
	return c.View(id), changed, nil
}
