package ui

import (
	"fmt"
	"strings"
)

// list holds the items of a list panel, the subset matching the filter, and
// the selected row. It knows nothing about gocui.
type list[T any] struct {
	items  []T
	shown  []int // indices into items that match the filter
	sel    int   // index into shown
	filter string
	key    func(T) string       // identity, used to keep the selection
	match  func(T, string) bool // q is lower-cased and non-empty
}

func newList[T any](key func(T) string, match func(T, string) bool) *list[T] {
	return &list[T]{key: key, match: match}
}

// set replaces the items, keeping the selection on the same key if present.
func (l *list[T]) set(items []T) {
	prev := l.selectedKey()
	l.items = items
	l.apply(prev)
}

// setFilter changes the filter, keeping the selection if it still matches.
func (l *list[T]) setFilter(f string) {
	prev := l.selectedKey()
	l.filter = f
	l.apply(prev)
}

func (l *list[T]) apply(prevKey string) {
	q := strings.ToLower(strings.TrimSpace(l.filter))
	l.shown = l.shown[:0]
	for i, it := range l.items {
		if q == "" || l.match(it, q) {
			l.shown = append(l.shown, i)
		}
	}
	l.sel = 0
	if prevKey != "" {
		l.selectKey(prevKey)
	}
}

func (l *list[T]) selectedKey() string {
	if it, ok := l.selected(); ok {
		return l.key(it)
	}
	return ""
}

// selectKey selects the shown item with key k and reports whether it exists.
func (l *list[T]) selectKey(k string) bool {
	for i, idx := range l.shown {
		if l.key(l.items[idx]) == k {
			l.sel = i
			return true
		}
	}
	return false
}

func (l *list[T]) selected() (T, bool) {
	var zero T
	if l.sel < 0 || l.sel >= len(l.shown) {
		return zero, false
	}
	return l.items[l.shown[l.sel]], true
}

// move moves the selection by delta, clamped to the shown items.
func (l *list[T]) move(delta int) {
	l.sel = max(0, min(l.sel+delta, len(l.shown)-1))
}

func (l *list[T]) len() int { return len(l.shown) }

func (l *list[T]) at(i int) T { return l.items[l.shown[i]] }

// footer is the "x of y" counter for the bottom border.
func (l *list[T]) footer() string {
	if len(l.shown) == 0 {
		return ""
	}
	return fmt.Sprintf("%d of %d", l.sel+1, len(l.shown))
}
