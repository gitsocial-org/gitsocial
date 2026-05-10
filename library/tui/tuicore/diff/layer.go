// layer.go - Composable post-build decoration layer.
//
// Layers contribute additional DisplayRows (e.g. PR feedback) or recolor
// existing rows. The view applies each layer in declared order after
// BuildPlan and before render.
package diff

// Layer post-processes a DisplayPlan after BuildPlan. Implementations
// (e.g. FeedbackLayer in tuireview) typically insert RowFeedback rows
// after specific anchor positions.
type Layer interface {
	Decorate(plan DisplayPlan, state ViewState) DisplayPlan
}
