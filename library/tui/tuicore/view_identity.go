// view_identity.go - Passive status view showing the workspace user's verification state
package tuicore

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/identity"
	"github.com/gitsocial-org/gitsocial/library/core/settings"
)

var CoreIdentity = RegisterContext("core.identity")

func init() {
	RegisterViewMeta(ViewMeta{Path: "/config/identity", Context: CoreIdentity, Title: "Identity", Icon: "⚿", NavItemID: "identity"})
}

// IdentityView shows the user's git signing config and the cached binding for
// their (signing key, email) pair. Verification of the user's own binding is
// driven by the background verifier; this view adds an on-demand email lookup
// (`r`) mirroring the `id resolve` CLI verb.
type IdentityView struct {
	workdir    string
	input      textinput.Model
	resolving  bool
	resolved   *identity.ResolvedIdentity
	resolveErr string
}

// NewIdentityView creates the identity status + lookup view.
func NewIdentityView(workdir string) *IdentityView {
	input := textinput.New()
	input.CharLimit = 254
	input.Prompt = "> "
	StyleTextInput(&input, Dim, lipgloss.NewStyle(), Dim)
	return &IdentityView{workdir: workdir, input: input}
}

// SetSize is unused for this view.
func (v *IdentityView) SetSize(width, height int) {}

// Activate is a no-op — the view reads fresh state on each render.
func (v *IdentityView) Activate(state *State) tea.Cmd { return nil }

// Deactivate is a no-op.
func (v *IdentityView) Deactivate() {}

// Update handles policy toggles and the on-demand email lookup.
func (v *IdentityView) Update(msg tea.Msg, state *State) tea.Cmd {
	switch msg := msg.(type) {
	case identityResolvedMsg:
		v.resolving = false
		v.input.Blur()
		if msg.err != nil {
			v.resolveErr = msg.err.Error()
			v.resolved = nil
		} else {
			v.resolveErr = ""
			v.resolved = msg.resolved
		}
		return nil
	case tea.KeyPressMsg:
		if v.resolving {
			switch msg.String() {
			case "esc":
				v.resolving = false
				v.input.Blur()
				return nil
			case "enter":
				email := strings.TrimSpace(v.input.Value())
				if email == "" {
					return nil
				}
				return doResolveIdentity(email)
			}
			var cmd tea.Cmd
			v.input, cmd = v.input.Update(msg)
			return cmd
		}
		switch msg.String() {
		case "d":
			v.toggleDNSVerification()
		case "r":
			v.resolving = true
			v.resolveErr = ""
			v.input.SetValue("")
			return v.input.Focus()
		}
	default:
		if v.resolving {
			var cmd tea.Cmd
			v.input, cmd = v.input.Update(msg)
			return cmd
		}
	}
	return nil
}

// IsInputActive returns true only while the email-resolve prompt is open.
func (v *IdentityView) IsInputActive() bool { return v.resolving }

// Bindings returns the policy-toggle and lookup keys.
func (v *IdentityView) Bindings() []Binding {
	noop := func(*HandlerContext) (bool, tea.Cmd) { return false, nil }
	return []Binding{
		{Key: "d", Label: "toggle DNS verification", Contexts: []Context{CoreIdentity}, Handler: noop},
		{Key: "r", Label: "resolve email", Contexts: []Context{CoreIdentity}, Handler: noop},
	}
}

// doResolveIdentity resolves an email to its declared identity via the DNS
// well-known endpoint (mirrors `gitsocial id resolve`). Runs async; reports
// through identityResolvedMsg.
func doResolveIdentity(email string) tea.Cmd {
	return func() tea.Msg {
		resolved, err := identity.ResolveIdentity(email)
		return identityResolvedMsg{resolved: resolved, err: err}
	}
}

// identityResolvedMsg carries the result of an async email resolve.
type identityResolvedMsg struct {
	resolved *identity.ResolvedIdentity
	err      error
}

// toggleDNSVerification flips identity.dns_verification through the settings
// Manager (lands in the personal repo) and syncs the in-process verifier flag.
func (v *IdentityView) toggleDNSVerification() {
	next := !identity.IsDNSVerificationEnabled()
	val := "false"
	if next {
		val = "true"
	}
	if err := settings.NewManager().Write("identity.dns_verification", val); err != nil {
		return
	}
	identity.SetDNSVerificationEnabled(next)
}

// Render renders the view.
func (v *IdentityView) Render(state *State) string {
	wrapper := NewViewWrapper(state)
	rs := RowStylesWithWidths(16, 0)
	verified := lipgloss.NewStyle().Foreground(lipgloss.Color(IdentityMe))

	var b strings.Builder

	b.WriteString(RenderHeader(rs, "Git Signing Configuration"))
	b.WriteString("\n")

	name := git.GetUserName(v.workdir)
	email := git.GetUserEmail(v.workdir)
	signingKey := git.GetGitConfig(v.workdir, "user.signingkey")
	signingFormat := git.GetGitConfig(v.workdir, "gpg.format")
	if signingFormat == "" {
		signingFormat = "gpg"
	}

	if name != "" {
		b.WriteString(RenderRow(rs, "Name", name, "", false))
		b.WriteString("\n")
	}
	if email != "" {
		b.WriteString(RenderRow(rs, "Email", email, "", false))
		b.WriteString("\n")
	} else {
		b.WriteString(RenderRow(rs, "Email", Dim.Render("not configured (set git config user.email)"), "", false))
		b.WriteString("\n")
	}
	if signingKey != "" {
		b.WriteString(RenderRow(rs, "Signing key", truncateKeyDisplay(signingKey), "", false))
		b.WriteString("\n")
		b.WriteString(RenderRow(rs, "Format", signingFormat, "", false))
		b.WriteString("\n")
	} else {
		b.WriteString(RenderRow(rs, "Signing key", Dim.Render("not configured (set git config user.signingkey)"), "", false))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(RenderHeader(rs, "Verification Status"))
	b.WriteString("\n")

	if email == "" || signingKey == "" {
		b.WriteString("  ")
		b.WriteString(Dim.Render("configure name, email, and signing key to enable verification"))
		b.WriteString("\n")
	} else {
		repoURL := gitmsg.ResolveRepoURL(v.workdir)
		signerKey := identity.FindUserSignerKey(repoURL, email)
		if signerKey == "" {
			gpgsign := git.GetGitConfig(v.workdir, "commit.gpgsign")
			if gpgsign != "true" {
				b.WriteString(RenderRow(rs, "Status", Dim.Render("commits are not auto-signed (set commit.gpgsign=true)"), "", false))
			} else {
				b.WriteString(RenderRow(rs, "Status", Dim.Render("no signed commits cached (run gitsocial fetch)"), "", false))
			}
			b.WriteString("\n")
		} else {
			binding := identity.LookupBinding(signerKey, email)
			statusText, ok := identityStatusText(binding)
			if ok {
				b.WriteString(RenderRow(rs, "Status", verified.Render(SafeIcon("⚿")+" "+statusText), "", false))
			} else {
				b.WriteString(RenderRow(rs, "Status", Dim.Render(statusText), "", false))
			}
			b.WriteString("\n")
			if binding != nil && binding.ForgeHost != "" {
				b.WriteString(RenderRow(rs, "Forge", binding.ForgeHost, "", false))
				b.WriteString("\n")
			}
			if binding != nil && binding.ForgeAccount != "" {
				b.WriteString(RenderRow(rs, "Account", binding.ForgeAccount, "", false))
				b.WriteString("\n")
			}
		}
	}

	b.WriteString("\n")
	b.WriteString(RenderHeader(rs, "Policies"))
	b.WriteString("\n")
	dnsState := "off"
	if identity.IsDNSVerificationEnabled() {
		dnsState = "on"
	}
	b.WriteString(RenderRow(rs, "DNS verification", dnsState+"  "+Dim.Render("(d to toggle)"), "", false))
	b.WriteString("\n")

	b.WriteString("\n")
	b.WriteString(RenderHeader(rs, "Identity Lookup"))
	b.WriteString("\n")
	if v.resolving {
		b.WriteString(RenderRow(rs, "Resolve email", v.input.View(), "", false))
		b.WriteString("\n")
	} else {
		b.WriteString(RenderRow(rs, "Resolve email", Dim.Render("(r to look up an email via DNS)"), "", false))
		b.WriteString("\n")
	}
	if v.resolveErr != "" {
		b.WriteString("  ")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(StatusError)).Render("Error: " + v.resolveErr))
		b.WriteString("\n")
	} else if v.resolved != nil {
		b.WriteString(RenderRow(rs, "  Email", v.resolved.Email, "", false))
		b.WriteString("\n")
		b.WriteString(RenderRow(rs, "  Key", truncateKeyDisplay(v.resolved.Key), "", false))
		b.WriteString("\n")
		b.WriteString(RenderRow(rs, "  Type", v.resolved.KeyType(), "", false))
		b.WriteString("\n")
		if v.resolved.Repo != "" {
			b.WriteString(RenderRow(rs, "  Repo", v.resolved.Repo, "", false))
			b.WriteString("\n")
		}
		src := "fetched"
		if v.resolved.Cached {
			src = "cached"
		}
		b.WriteString(RenderRow(rs, "  Source", src, "", false))
		b.WriteString("\n")
	}

	footer := RenderFooter(state.Registry, CoreIdentity, nil)
	if v.resolving {
		footer = Dim.Render("type an email · enter to resolve · esc to cancel")
	}
	return wrapper.Render(b.String(), footer)
}

// identityStatusText returns a short status label and whether it represents a
// verified state (suitable for ⚿ rendering).
func identityStatusText(b *identity.Binding) (string, bool) {
	if b == nil {
		return "no cached attestation (run gitsocial fetch)", false
	}
	if !b.Verified {
		return "no attestation source affirmed this binding", false
	}
	switch b.Source {
	case identity.SourceForgeGPG:
		return "verified (forge GPG)", true
	case identity.SourceForgeAPI:
		return "verified (forge API)", true
	case identity.SourceDNS:
		return "verified (DNS)", true
	default:
		return "verified", true
	}
}

// truncateKeyDisplay shortens a key for display.
func truncateKeyDisplay(key string) string {
	parts := strings.Fields(key)
	if len(parts) < 2 {
		if len(key) > 32 {
			return key[:16] + "..." + key[len(key)-12:]
		}
		return key
	}
	b64 := parts[1]
	if len(b64) > 16 {
		b64 = b64[:8] + "..." + b64[len(b64)-4:]
	}
	return parts[0] + " " + b64
}
