package search

// computeOffset returns the item offset for a page.
func computeOffset(page, size int) int {
	return (page - 1) * size
}
