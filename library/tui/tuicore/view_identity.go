// view_identity.go - Passive status view showing the workspace user's verification state
package tuicore

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/identity"
)

var CoreIdentity = RegisterContext("core.identity")

func init() {
	RegisterViewMeta(ViewMeta{Path: "/config/identity", Context: CoreIdentity, Title: "Identity", Icon: "⚿", NavItemID: "config.identity"})
}

// IdentityView shows the user's git signing config and the cached binding for
// their (signing key, email) pair. Verification is controlled entirely by the
// background verifier — this view is read-only.
type IdentityView struct {
	workdir string
}

// NewIdentityView creates the read-only identity status view.
func NewIdentityView(workdir string) *IdentityView {
	return &IdentityView{workdir: workdir}
}

// SetSize is unused for this view.
func (v *IdentityView) SetSize(width, height int) {}

// Activate is a no-op — the view reads fresh state on each render.
func (v *IdentityView) Activate(state *State) tea.Cmd { return nil }

// Deactivate is a no-op.
func (v *IdentityView) Deactivate() {}

// Update handles messages — none for this read-only view.
func (v *IdentityView) Update(msg tea.Msg, state *State) tea.Cmd { return nil }

// IsInputActive returns false — the view never owns input.
func (v *IdentityView) IsInputActive() bool { return false }

// Bindings returns no key bindings.
func (v *IdentityView) Bindings() []Binding { return nil }

// Render renders the view.
func (v *IdentityView) Render(state *State) string {
	wrapper := NewViewWrapper(state)
	rs := RowStylesWithWidths(14, 0)
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

	footer := RenderFooter(state.Registry, CoreIdentity, wrapper.ContentWidth(), nil)
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
