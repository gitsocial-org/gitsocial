// view_analytics.go - Analytics dashboard with sparklines, bar charts, and extension sections
package tuicore

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
)

var (
	sparkChars   = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	analyticsBar = lipgloss.NewStyle().Foreground(lipgloss.Color(IdentityFollowing)).Faint(true)
)

// AnalyticsView displays commit activity, repository rankings, and extension stats.
type AnalyticsView struct {
	data        *cache.AnalyticsData
	err         string
	scroll      int
	repoURL     string // always set (workspace origin or specific repo)
	workdir     string // workspace directory for list-scoped network queries
	showNetwork bool   // true when opened from nav panel (shows cross-repo rankings)
	network     *cache.NetworkAnalytics
}

// AnalyticsLoadedMsg is sent when analytics data is loaded.
type AnalyticsLoadedMsg struct {
	Data    *cache.AnalyticsData
	Err     error
	Network *cache.NetworkAnalytics
}

// NewAnalyticsView creates a new analytics view.
func NewAnalyticsView() *AnalyticsView {
	return &AnalyticsView{}
}

// Bindings returns keybindings for the analytics view.
func (v *AnalyticsView) Bindings() []Binding {
	noop := func(ctx *HandlerContext) (bool, tea.Cmd) { return false, nil }
	return []Binding{
		{Key: "r", Label: "refresh", Contexts: []Context{Analytics}, Handler: noop},
		{Key: "j", Label: "scroll down", Contexts: []Context{Analytics}, Handler: noop},
		{Key: "k", Label: "scroll up", Contexts: []Context{Analytics}, Handler: noop},
		{Key: "ctrl+d", Label: "half-page down", Contexts: []Context{Analytics}, Handler: noop},
		{Key: "ctrl+u", Label: "half-page up", Contexts: []Context{Analytics}, Handler: noop},
		{Key: "home", Label: "top", Contexts: []Context{Analytics}, Handler: noop},
		{Key: "end", Label: "bottom", Contexts: []Context{Analytics}, Handler: noop},
	}
}

// Activate loads analytics data when the view becomes active.
func (v *AnalyticsView) Activate(state *State) tea.Cmd {
	v.scroll = 0
	v.workdir = state.Workdir
	v.repoURL = state.Router.Location().Param("url")
	if v.repoURL == "" {
		v.repoURL = gitmsg.ResolveRepoURL(state.Workdir)
		v.showNetwork = true
	} else {
		v.showNetwork = false
	}
	return v.loadAnalytics()
}

// Title returns the panel title for the analytics view.
func (v *AnalyticsView) Title() string {
	if v.showNetwork {
		return ""
	}
	name := shortRepoName(v.repoURL)
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	return "◧  " + name
}

// loadAnalytics loads analytics data from cache.
func (v *AnalyticsView) loadAnalytics() tea.Cmd {
	repoURL := v.repoURL
	workdir := v.workdir
	showNetwork := v.showNetwork
	return func() tea.Msg {
		data, err := cache.GetAnalytics(repoURL)
		msg := AnalyticsLoadedMsg{Data: data, Err: err}
		if showNetwork {
			msg.Network = cache.GetNetworkAnalytics(workdir)
		}
		return msg
	}
}

// Update handles messages.
func (v *AnalyticsView) Update(msg tea.Msg, state *State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		switch msg.(type) {
		case tea.MouseWheelMsg:
			m := msg.Mouse()
			if m.Button == tea.MouseWheelUp {
				v.scroll -= 3
				if v.scroll < 0 {
					v.scroll = 0
				}
			} else {
				v.scroll += 3
			}
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "r":
			return v.loadAnalytics()
		case "j", "down":
			v.scroll++
		case "k", "up":
			if v.scroll > 0 {
				v.scroll--
			}
		case "ctrl+d", "pgdown":
			v.scroll += state.InnerHeight() / 2
		case "ctrl+u", "pgup":
			v.scroll -= state.InnerHeight() / 2
			if v.scroll < 0 {
				v.scroll = 0
			}
		case "home", "g":
			v.scroll = 0
		case "end", "G":
			v.scroll = 99999
		}
	case AnalyticsLoadedMsg:
		if msg.Err != nil {
			v.err = msg.Err.Error()
			return nil
		}
		v.data = msg.Data
		v.network = msg.Network
		v.err = ""
	}
	return nil
}

// Render renders the analytics view.
func (v *AnalyticsView) Render(state *State) string {
	wrapper := NewViewWrapper(state)
	if v.data == nil {
		content := Dim.Render("Loading analytics...")
		footer := RenderFooter(state.Registry, Analytics, wrapper.ContentWidth(), nil)
		return wrapper.Render(content, footer)
	}

	d := v.data
	contentWidth := wrapper.ContentWidth()
	rs := DefaultRowStyles()
	var b strings.Builder

	// --- Commits sparkline ---
	b.WriteString(rs.Header.Render("Commits (30 days)"))
	b.WriteString("\n\n")

	if d.TotalCommits == 0 && len(d.CommitsPerDay) == 0 {
		b.WriteString(sparkline(fillDays(nil)))
		b.WriteString("\n\n")
		b.WriteString(Dim.Render("0 commits · 0 repos tracked"))
		b.WriteString("\n\n")
		b.WriteString(Dim.Render("Follow a repository or create a post to see activity here."))
	} else {
		values := fillDays(d.CommitsPerDay)
		b.WriteString(sparkline(values))
		b.WriteString("\n")
		b.WriteString(Dim.Render(dayLabels(30)))
		b.WriteString("\n\n")
		avg := d.TotalCommits / 30
		busiest := busiestDay(d.DayOfWeek)
		summary := fmt.Sprintf("%d total %s  ·  avg %d/day  ·  busiest: %s",
			d.TotalCommits, trend(d.TotalCommits, d.PrevTotalCommits),
			avg, busiest)
		b.WriteString(Dim.Render(summary))
		b.WriteString("\n\n\n")

		// --- Contributors ---
		b.WriteString(rs.Header.Render("Contributors"))
		b.WriteString("\n\n")
		if len(d.Contributors) > 0 {
			maxCount := d.Contributors[0].Count
			barWidth := contentWidth - 55
			if barWidth < 10 {
				barWidth = 10
			}
			for _, c := range d.Contributors {
				nameStr := fmt.Sprintf("%-16s", truncate(c.Name, 16))
				emailStr := ""
				if state.ShowEmailOnCards {
					emailStr = fmt.Sprintf("%-20s", truncate(c.Email, 20))
				}
				barStr := bar(c.Count, maxCount, barWidth)
				line := fmt.Sprintf("  %s  %s%s  %4d",
					rs.Value.Render(nameStr),
					Dim.Render(emailStr),
					analyticsBar.Render(barStr),
					c.Count)
				b.WriteString(line)
				b.WriteString("\n")
			}
			b.WriteString("\n")
			b.WriteString(Dim.Render(fmt.Sprintf("  %d contributors across %d repos", d.TotalContributors, d.ActiveRepos)))
		} else {
			b.WriteString(Dim.Render("  No contributor activity"))
		}
		b.WriteString("\n\n\n")

		// --- Day of week ---
		b.WriteString(rs.Header.Render("Activity by Day"))
		b.WriteString("\n\n")
		dayNames := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
		maxDow := 0
		for _, c := range d.DayOfWeek {
			if c > maxDow {
				maxDow = c
			}
		}
		dowBarWidth := contentWidth - 18
		if dowBarWidth < 10 {
			dowBarWidth = 10
		}
		for i, name := range dayNames {
			barStr := bar(d.DayOfWeek[i], maxDow, dowBarWidth)
			fmt.Fprintf(&b, "  %s  %s  %d",
				rs.Label.Width(3).Render(name),
				analyticsBar.Render(barStr),
				d.DayOfWeek[i])
			b.WriteString("\n")
		}

		// --- Extension sections ---
		if d.Social != nil {
			b.WriteString("\n")
			b.WriteString(sectionDivider("Social", contentWidth))
			b.WriteString("\n\n")
			v.renderSocial(&b, d.Social, rs)
		}

		if d.PM != nil {
			b.WriteString("\n")
			b.WriteString(sectionDivider("Project Management", contentWidth))
			b.WriteString("\n\n")
			v.renderPM(&b, d.PM, rs, contentWidth)
		}

		if d.Release != nil {
			b.WriteString("\n")
			b.WriteString(sectionDivider("Releases", contentWidth))
			b.WriteString("\n\n")
			v.renderRelease(&b, d.Release, rs)
		}

		if d.Review != nil {
			b.WriteString("\n")
			b.WriteString(sectionDivider("Code Review", contentWidth))
			b.WriteString("\n\n")
			v.renderReview(&b, d.Review, rs, contentWidth)
		}

		// --- Network section (nav panel only) ---
		if v.showNetwork && v.network != nil {
			b.WriteString("\n\n\n")
			netBold := lipgloss.NewStyle().Foreground(lipgloss.Color(TextPrimary)).Bold(true)
			netLine := netBold.Render(strings.Repeat("═", contentWidth))
			b.WriteString(netLine)
			b.WriteString("\n")
			b.WriteString(netBold.Render("NETWORK"))
			b.WriteString("\n")
			b.WriteString(netLine)
			b.WriteString("\n\n")
			v.renderNetwork(&b, rs, contentWidth)
		}
	}

	// Apply scroll offset
	lines := strings.Split(b.String(), "\n")
	height := wrapper.ContentHeight()
	if v.scroll >= len(lines) {
		v.scroll = len(lines) - 1
	}
	if v.scroll < 0 {
		v.scroll = 0
	}
	end := v.scroll + height
	if end > len(lines) {
		end = len(lines)
	}
	visible := strings.Join(lines[v.scroll:end], "\n")

	footer := v.renderFooter(state, wrapper.ContentWidth())
	return wrapper.Render(visible, footer)
}

// renderSocial renders the social extension section.
func (v *AnalyticsView) renderSocial(b *strings.Builder, sa *cache.SocialAnalytics, rs RowStyles) {
	b.WriteString(rs.Header.Render("Posts (30 days)"))
	b.WriteString("\n\n")
	for _, item := range []struct {
		label string
		total int
		prev  int
		data  []cache.DayStat
	}{
		{"Posts", sa.TotalPosts, sa.PrevPosts, sa.PostsPerDay},
		{"Comments", sa.TotalComments, sa.PrevComments, sa.CommentsPerDay},
		{"Reposts", sa.TotalReposts, sa.PrevReposts, sa.RepostsPerDay},
	} {
		values := fillDays(item.data)
		fmt.Fprintf(b, "  %-10s  %4d %s    %s",
			item.label, item.total, trendColor(trend(item.total, item.prev)),
			sparkline(values))
		b.WriteString("\n")
	}
}

// renderPM renders the PM extension section.
func (v *AnalyticsView) renderPM(b *strings.Builder, pa *cache.PMAnalytics, rs RowStyles, width int) {
	b.WriteString(rs.Header.Render("Issues"))
	b.WriteString("\n\n")
	barWidth := 30
	if barWidth > width-40 {
		barWidth = width - 40
	}
	fmt.Fprintf(b, "  %s  %d open / %d closed",
		ratioBar(pa.OpenIssues, pa.ClosedIssues, barWidth),
		pa.OpenIssues, pa.ClosedIssues)
	b.WriteString("\n\n")

	openedValues := fillDays(pa.OpenedPerDay)
	closedValues := fillDays(pa.ClosedPerDay)
	openedTotal := sum(openedValues)
	closedTotal := sum(closedValues)

	fmt.Fprintf(b, "  %-8s  %s  %d this month", "Opened", sparkline(openedValues), openedTotal)
	b.WriteString("\n")
	fmt.Fprintf(b, "  %-8s  %s  %d this month", "Closed", sparkline(closedValues), closedTotal)
	b.WriteString("\n")
	if pa.AvgAgeDays > 0 {
		fmt.Fprintf(b, "  Avg age  %d days", pa.AvgAgeDays)
		b.WriteString("\n")
	}

	if len(pa.Milestones) > 0 {
		b.WriteString("\n")
		b.WriteString(rs.Header.Render("Milestones"))
		b.WriteString("\n\n")
		for _, ms := range pa.Milestones {
			pct := 0
			if ms.Total > 0 {
				pct = (ms.Closed * 100) / ms.Total
			}
			name := truncate(ms.Name, 20)
			line := fmt.Sprintf("  %-20s  %s  %d%%", name, progressBar(pct, 30), pct)
			if ms.Due != "" {
				if due, err := time.Parse("2006-01-02", ms.Due); err == nil {
					days := int(time.Until(due).Hours() / 24)
					line += fmt.Sprintf("  due %s (%d days)", due.Format("Jan 2"), days)
				}
			}
			b.WriteString(Dim.Render(line))
			b.WriteString("\n")
		}
	}
}

// renderRelease renders the release extension section.
func (v *AnalyticsView) renderRelease(b *strings.Builder, ra *cache.ReleaseAnalytics, rs RowStyles) {
	b.WriteString(rs.Header.Render("Releases"))
	b.WriteString("\n\n")
	if len(ra.Recent) > 0 {
		parts := make([]string, 0, len(ra.Recent))
		for _, r := range ra.Recent {
			parts = append(parts, fmt.Sprintf("%s  %s", rs.Value.Render(r.Version), Dim.Render(FormatTime(r.Timestamp))))
		}
		b.WriteString("  " + strings.Join(parts, "     "))
		b.WriteString("\n\n")
	}
	if len(ra.FreqPerDay) > 0 {
		values := fillDays(ra.FreqPerDay)
		perMonth := ""
		if ra.PerMonth > 0 {
			perMonth = fmt.Sprintf(" · ~%.0f/month", ra.PerMonth)
		}
		fmt.Fprintf(b, "  Frequency  %s  %d total%s", sparkline(values), ra.Total, perMonth)
		b.WriteString("\n")
	}
}

// renderReview renders the review extension section.
func (v *AnalyticsView) renderReview(b *strings.Builder, rva *cache.ReviewAnalytics, rs RowStyles, width int) {
	b.WriteString(rs.Header.Render("Pull Requests"))
	b.WriteString("\n\n")
	barWidth := 30
	if barWidth > width-40 {
		barWidth = width - 40
	}
	fmt.Fprintf(b, "  %s  %d open / %d merged",
		ratioBar(rva.OpenPRs, rva.MergedPRs, barWidth),
		rva.OpenPRs, rva.MergedPRs)
	if rva.ClosedPRs > 0 {
		fmt.Fprintf(b, " / %d closed", rva.ClosedPRs)
	}
	b.WriteString("\n\n")
	if len(rva.PRsPerDay) > 0 {
		values := fillDays(rva.PRsPerDay)
		total := sum(values)
		fmt.Fprintf(b, "  %-8s  %s  %d this month", "PRs", sparkline(values), total)
		b.WriteString("\n")
	}
	if rva.TotalFeedback > 0 {
		fmt.Fprintf(b, "  Feedback  %d reviews", rva.TotalFeedback)
		b.WriteString("\n")
	}
}

// renderNetwork renders the cross-repo network rankings section.
func (v *AnalyticsView) renderNetwork(b *strings.Builder, rs RowStyles, contentWidth int) {
	na := v.network
	barColor := analyticsBar
	barWidth := contentWidth - 50
	if barWidth < 10 {
		barWidth = 10
	}

	// Repositories by commits
	if len(na.Repos) > 0 {
		b.WriteString(rs.Header.Render("Repositories"))
		b.WriteString("\n\n")
		maxCount := na.Repos[0].Count
		for _, repo := range na.Repos {
			name := shortRepoName(repo.URL)
			nameStr := fmt.Sprintf("%-20s", truncate(name, 20))
			barStr := bar(repo.Count, maxCount, barWidth)
			trendStr := trend(repo.Count, repo.PrevCount)
			lastStr := ""
			if !repo.LastSeen.IsZero() {
				lastStr = FormatTime(repo.LastSeen)
			}
			fmt.Fprintf(b, "  %s  %s  %4d  %s  %s",
				rs.Label.Render(nameStr), barColor.Render(barStr),
				repo.Count, trendColor(trendStr), Dim.Render(lastStr))
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(Dim.Render(fmt.Sprintf("  %d repos tracked · %d active this month", na.TrackedRepos, na.ActiveRepos)))
		b.WriteString("\n\n\n")
	}

	// Posts by repo
	if na.Social != nil && len(na.Social.RepoActivity) > 0 {
		b.WriteString(rs.Header.Render("Posts"))
		b.WriteString("\n\n")
		maxPosts := na.Social.RepoActivity[0].Posts
		for _, repo := range na.Social.RepoActivity {
			name := shortRepoName(repo.URL)
			nameStr := fmt.Sprintf("%-20s", truncate(name, 20))
			barStr := bar(repo.Posts, maxPosts, barWidth)
			trendStr := trend(repo.Posts, repo.PrevPosts)
			fmt.Fprintf(b, "  %s  %s  %4d  %s",
				rs.Label.Render(nameStr), barColor.Render(barStr),
				repo.Posts, trendColor(trendStr))
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(Dim.Render(fmt.Sprintf("  %d posts · %d comments · %d reposts this month",
			na.Social.TotalPosts, na.Social.TotalComments, na.Social.TotalReposts)))
		b.WriteString("\n\n\n")
	}

	// Issues by repo
	if na.PM != nil && len(na.PM.RepoActivity) > 0 {
		b.WriteString(rs.Header.Render("Issues"))
		b.WriteString("\n\n")
		issueBarWidth := 30
		if issueBarWidth > contentWidth-40 {
			issueBarWidth = contentWidth - 40
		}
		for _, repo := range na.PM.RepoActivity {
			name := shortRepoName(repo.URL)
			nameStr := fmt.Sprintf("%-20s", truncate(name, 20))
			ratioStr := ratioBar(repo.Open, repo.Closed, issueBarWidth)
			fmt.Fprintf(b, "  %s  %s  %d open / %d closed",
				rs.Label.Render(nameStr), ratioStr, repo.Open, repo.Closed)
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(Dim.Render(fmt.Sprintf("  %d open · %d closed across network",
			na.PM.TotalOpen, na.PM.TotalClosed)))
		b.WriteString("\n\n\n")
	}

	// Recent releases
	if na.Release != nil && len(na.Release.Recent) > 0 {
		b.WriteString(rs.Header.Render("Releases"))
		b.WriteString("\n\n")
		for _, rel := range na.Release.Recent {
			name := shortRepoName(rel.URL)
			fmt.Fprintf(b, "  %s  %s  %s",
				rs.Label.Render(truncate(name, 20)),
				rs.Value.Render(rel.Version),
				Dim.Render(FormatTime(rel.Timestamp)))
			b.WriteString("\n")
		}
		if na.Release.TotalMonth > 0 {
			b.WriteString("\n")
			b.WriteString(Dim.Render(fmt.Sprintf("  %d releases this month across %d repos",
				na.Release.TotalMonth, na.Release.RepoCount)))
		}
		b.WriteString("\n\n\n")
	}

	// PRs by repo
	if na.Review != nil && len(na.Review.RepoActivity) > 0 {
		b.WriteString(rs.Header.Render("Pull Requests"))
		b.WriteString("\n\n")
		issueBarWidth := 30
		if issueBarWidth > contentWidth-40 {
			issueBarWidth = contentWidth - 40
		}
		for _, repo := range na.Review.RepoActivity {
			name := shortRepoName(repo.URL)
			nameStr := fmt.Sprintf("%-20s", truncate(name, 20))
			ratioStr := ratioBar(repo.Open, repo.Merged, issueBarWidth)
			fmt.Fprintf(b, "  %s  %s  %d open / %d merged",
				rs.Label.Render(nameStr), ratioStr, repo.Open, repo.Merged)
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(Dim.Render(fmt.Sprintf("  %d open · %d merged across network",
			na.Review.TotalOpen, na.Review.TotalMerged)))
	}
}

// renderFooter renders the analytics footer.
func (v *AnalyticsView) renderFooter(state *State, width int) string {
	return RenderFooter(state.Registry, Analytics, width, nil)
}

// sectionDivider renders a labeled divider line: ───── Label ─────
func sectionDivider(label string, width int) string {
	prefixWidth := 6 // "───── " = 5 dashes + space
	suffixWidth := 6 // " ─────" = space + 5 dashes
	remaining := width - prefixWidth - len(label) - suffixWidth
	if remaining < 0 {
		remaining = 0
	}
	return Dim.Render("───── " + label + " " + strings.Repeat("─", 5+remaining))
}

// --- Rendering helpers ---

// sparkline renders values as a sparkline string using block characters.
func sparkline(values []int) string {
	max := 0
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	var b strings.Builder
	for _, v := range values {
		idx := 0
		if max > 0 {
			idx = (v * 7) / max
		}
		b.WriteRune(sparkChars[idx])
	}
	return b.String()
}

// bar renders a horizontal bar chart segment.
func bar(value, max, width int) string {
	if max == 0 || width <= 0 {
		return ""
	}
	filled := (value * width) / max
	if filled == 0 && value > 0 {
		filled = 1
	}
	return strings.Repeat("█", filled)
}

// ratioBar renders a filled/unfilled ratio bar.
func ratioBar(a, b, width int) string {
	total := a + b
	if total == 0 {
		return strings.Repeat("░", width)
	}
	filled := (a * width) / total
	return analyticsBar.Render(strings.Repeat("█", filled)) +
		Dim.Render(strings.Repeat("░", width-filled))
}

// progressBar renders a progress bar with filled and unfilled portions.
func progressBar(pct, width int) string {
	filled := (pct * width) / 100
	return analyticsBar.Render(strings.Repeat("█", filled)) +
		Dim.Render(strings.Repeat("░", width-filled))
}

// trend returns a trend indicator comparing current to previous period.
func trend(current, previous int) string {
	if previous == 0 {
		if current > 0 {
			return "↑"
		}
		return "→"
	}
	pct := ((current - previous) * 100) / previous
	switch {
	case pct > 5:
		return fmt.Sprintf("↑%d%%", pct)
	case pct < -5:
		return fmt.Sprintf("↓%d%%", -pct)
	default:
		return "→"
	}
}

// trendColor applies color to a trend string.
func trendColor(t string) string {
	if strings.HasPrefix(t, "↑") {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(StatusSuccess)).Render(t)
	}
	if strings.HasPrefix(t, "↓") {
		return Dim.Render(t)
	}
	return Dim.Render(t)
}

// fillDays fills a 30-element array from sparse DayStat data, aligning to dates.
func fillDays(stats []cache.DayStat) []int {
	const days = 30
	values := make([]int, days)
	if len(stats) == 0 {
		return values
	}
	now := time.Now().UTC()
	dateMap := make(map[string]int)
	for _, s := range stats {
		dateMap[s.Date] = s.Count
	}
	for i := 0; i < days; i++ {
		date := now.AddDate(0, 0, -(days - 1 - i)).Format("2006-01-02")
		values[i] = dateMap[date]
	}
	return values
}

// dayLabels returns abbreviated day names aligned under a 30-char sparkline.
func dayLabels(days int) string {
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -(days - 1))
	labels := make([]byte, days)
	for i := 0; i < days; i++ {
		d := start.AddDate(0, 0, i)
		if d.Weekday() == time.Monday {
			labels[i] = 'M'
		} else {
			labels[i] = ' '
		}
	}
	return string(labels)
}

// busiestDay returns the name of the busiest day of the week.
func busiestDay(dow [7]int) string {
	days := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	maxIdx := 0
	for i, c := range dow {
		if c > dow[maxIdx] {
			maxIdx = i
		}
	}
	return days[maxIdx]
}

// shortRepoName extracts a short name from a repository URL.
func shortRepoName(url string) string {
	// Remove protocol
	name := url
	for _, prefix := range []string{"https://", "http://", "git://", "ssh://"} {
		name = strings.TrimPrefix(name, prefix)
	}
	// Remove common hosts
	for _, host := range []string{"github.com/", "gitlab.com/", "bitbucket.org/"} {
		name = strings.TrimPrefix(name, host)
	}
	name = strings.TrimSuffix(name, ".git")
	return name
}

// truncate truncates a string to max length with ellipsis.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

// sum returns the sum of an int slice.
func sum(values []int) int {
	total := 0
	for _, v := range values {
		total += v
	}
	return total
}
