// social.go - GitLab social stub (no Discussions equivalent)
package gitlab

import importpkg "github.com/gitsocial-org/gitsocial/import"

// FetchSocial returns an empty plan — GitLab has no Discussions feature.
func (a *Adapter) FetchSocial(opts importpkg.FetchOptions) (*importpkg.SocialPlan, error) {
	return &importpkg.SocialPlan{}, nil
}
