// util_pagination.go - Shared cursor-based infinite scroll pagination
package tuicore

import tea "charm.land/bubbletea/v2"

// PageSize is the standard page size for all paginated views.
const PageSize = 100

// Pagination tracks cursor-based infinite scroll state.
type Pagination struct {
	HasMore      bool
	Loading      bool
	Cursor       string
	totalCount   int // total items across all pages
	refreshLimit int // temporary enlarged limit for refresh
}

// CanLoadMore returns true if more items are available and no load is in progress.
func (p *Pagination) CanLoadMore() bool { return p.HasMore && !p.Loading }

// Reset clears pagination state for a full reload.
func (p *Pagination) Reset() {
	p.HasMore = false
	p.Loading = false
	p.Cursor = ""
	p.totalCount = 0
	p.refreshLimit = 0
}

// SetTotal stores the total item count for header display.
func (p *Pagination) SetTotal(n int) {
	if n > 0 {
		p.totalCount = n
	}
}

// Total returns the best known total: the stored count if larger than loaded, otherwise loaded.
func (p *Pagination) Total(loaded int) int {
	if p.totalCount > loaded {
		return p.totalCount
	}
	return loaded
}

// ResetForRefresh clears pagination but preserves enough items to maintain scroll position.
func (p *Pagination) ResetForRefresh(currentCount int) {
	p.HasMore = false
	p.Loading = false
	p.Cursor = ""
	if currentCount > PageSize {
		p.refreshLimit = currentCount
	} else {
		p.refreshLimit = 0
	}
}

// Limit returns the page size for the initial load (enlarged after refresh).
func (p *Pagination) Limit() int {
	if p.refreshLimit > 0 {
		return p.refreshLimit
	}
	return PageSize
}

// StartLoading marks a page load as in progress.
func (p *Pagination) StartLoading() { p.Loading = true }

// Done updates state after a page loads. cursor is the RFC3339 timestamp of the last item.
func (p *Pagination) Done(hasMore bool, cursor string) {
	p.Loading = false
	p.HasMore = hasMore
	p.Cursor = cursor
	p.refreshLimit = 0
}

// LoadMore runs fn if pagination can load more, otherwise returns nil.
func (p *Pagination) LoadMore(fn func() tea.Cmd) tea.Cmd {
	if !p.CanLoadMore() {
		return nil
	}
	return fn()
}

// TrimPage detects hasMore using the limit+1 pattern.
func TrimPage[T any](items []T, pageSize int) ([]T, bool) {
	if len(items) > pageSize {
		return items[:pageSize], true
	}
	return items, false
}
