// avatars.go - GitHub lookups of the picture of an author
package github

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	importpkg "github.com/gitsocial-org/gitsocial/library/import"
)

// noreplyRe matches login@users.noreply.github.com and id+login@users.noreply.github.com.
var noreplyRe = regexp.MustCompile(`^(?:\d+\+)?([A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)@users\.noreply\.github\.com$`)

// avatarOriginRe splits a GitHub item URL into its kind, its number or tag, and its fragment.
var avatarOriginRe = regexp.MustCompile(`^https://github\.com/[^/]+/[^/]+/(issues|pull|releases/tag|discussions)/([^#]+)(#.*)?$`)

// ParseNoreply returns the login of a GitHub noreply address.
func ParseNoreply(email string) (login string, ok bool) {
	m := noreplyRe.FindStringSubmatch(strings.ToLower(strings.TrimSpace(email)))
	if m == nil {
		return "", false
	}
	return m[1], true
}

// AvatarURL returns the 80 px picture URL of an author: a noreply address with no call, else one gh api call on the first origin URL with a lookup, else on the commit sha; "" when nothing names the author or GitHub has no such item.
func AvatarURL(slug, email string, originURLs []string, sha string) (string, error) {
	if login, ok := ParseNoreply(email); ok {
		return "https://github.com/" + login + ".png?size=80", nil
	}
	path, field := "", ""
	for _, originURL := range originURLs {
		if path, field = avatarOriginLookup(slug, originURL); path != "" {
			break
		}
	}
	if path == "" && sha != "" {
		path, field = "repos/"+slug+"/commits/"+sha, ".author.avatar_url"
	}
	if path == "" {
		return "", nil
	}
	out, err := gh("api", path, "--jq", field)
	if err != nil {
		if isRetryableError(err.Error()) {
			return "", fmt.Errorf("%w: %v", importpkg.ErrRateLimited, err)
		}
		// A gone item (404) or a sha the forge does not hold (422) is a definite miss.
		if strings.Contains(err.Error(), "HTTP 404") || strings.Contains(err.Error(), "HTTP 422") {
			return "", nil
		}
		return "", err
	}
	src := strings.TrimSpace(string(out))
	if src == "" || src == "null" {
		return "", nil
	}
	if strings.Contains(src, "?") {
		return src + "&s=80", nil
	}
	return src + "?s=80", nil
}

// avatarOriginLookup returns the API path and the jq field of the avatar of the author an origin URL names; "" when no lookup applies.
func avatarOriginLookup(slug, originURL string) (path, field string) {
	m := avatarOriginRe.FindStringSubmatch(originURL)
	if m == nil {
		return "", ""
	}
	kind, id, fragment := m[1], m[2], m[3]
	switch {
	case kind == "discussions":
		return "", ""
	case kind == "releases/tag" && fragment == "":
		return "repos/" + slug + "/releases/tags/" + url.PathEscape(id), ".author.avatar_url"
	case fragment == "":
		return "repos/" + slug + "/issues/" + id, ".user.avatar_url"
	case strings.HasPrefix(fragment, "#issuecomment-"):
		return "repos/" + slug + "/issues/comments/" + strings.TrimPrefix(fragment, "#issuecomment-"), ".user.avatar_url"
	}
	return "", ""
}
