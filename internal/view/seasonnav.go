package view

import (
	"context"

	"github.com/Adyn-Cox/CoorsHeavy/internal/store"
)

// SeasonNav is what the header's season picker needs. It travels in the request
// context rather than as a parameter on every page component, for the same
// reason auth.IsAdmin does: the layout needs it, and threading it through the
// signature of every page to reach the layout would be noise on all of them.
type SeasonNav struct {
	Current store.Season
	All     []store.Season // newest first, as ListSeasons returns them
	// Path is the page the picker submits back to, so switching seasons keeps
	// you where you are instead of bouncing to the schedule.
	Path string
}

type seasonNavKey struct{}

// WithSeasonNav attaches the picker's data to a request context.
func WithSeasonNav(ctx context.Context, nav SeasonNav) context.Context {
	return context.WithValue(ctx, seasonNavKey{}, nav)
}

// seasonNav reads it back. The second result is false on pages that aren't
// season-scoped at all (login), which simply don't show a picker.
func seasonNav(ctx context.Context) (SeasonNav, bool) {
	nav, ok := ctx.Value(seasonNavKey{}).(SeasonNav)
	return nav, ok
}

// showSeasonPicker reports whether the header should render the picker. One
// season means there is nothing to switch between, and a bare dropdown with a
// single entry reads as a control that doesn't work.
func showSeasonPicker(ctx context.Context) bool {
	nav, ok := seasonNav(ctx)
	return ok && len(nav.All) > 1
}

// seasonPickerPath is the URL the picker posts back to.
func seasonPickerPath(ctx context.Context) string {
	nav, _ := seasonNav(ctx)
	if nav.Path == "" {
		return "/"
	}
	return nav.Path
}

// seasonPickerOptions lists every season, newest first.
func seasonPickerOptions(ctx context.Context) []store.Season {
	nav, _ := seasonNav(ctx)
	return nav.All
}

// seasonPickerSelected is the season being viewed.
func seasonPickerSelected(ctx context.Context) store.Season {
	nav, _ := seasonNav(ctx)
	return nav.Current
}

// seasonPickerNote is the league and field of the season on screen, so the
// header says what you are looking at and not only which year it was.
func seasonPickerNote(ctx context.Context) string {
	nav, _ := seasonNav(ctx)
	var parts []string
	if nav.Current.League != "" {
		parts = append(parts, nav.Current.League)
	}
	if nav.Current.Location != "" {
		parts = append(parts, nav.Current.Location)
	}
	return joinDot(parts)
}

// seasonOptionLabel marks the season the site defaults to.
func seasonOptionLabel(s store.Season) string {
	if s.IsCurrent {
		return s.Name + " (current)"
	}
	return s.Name
}
