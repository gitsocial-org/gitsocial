// repository.go - Repository discovery and related repository queries
package social

import (
	"sort"
	"strings"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

// GetRepositories retrieves repositories based on scope (all or by list).
func GetRepositories(workdir, scope string, limit int) Result[[]Repository] {
	var res Result[[]Repository]
	if strings.HasPrefix(scope, "list:") {
		listID := strings.TrimPrefix(scope, "list:")
		res = getRepositoriesByList(workdir, listID)
	} else {
		res = getAllRepositories(workdir)
	}
	if res.Success && limit > 0 && len(res.Data) > limit {
		res.Data = res.Data[:limit]
	}
	return res
}

// getRepositoriesByList retrieves repositories belonging to a specific list.
func getRepositoriesByList(workdir, listID string) Result[[]Repository] {
	data, err := gitmsg.ReadList(workdir, socialExtension, listID)
	if err != nil {
		return FailureWithDetails[[]Repository]("GIT_ERROR", "Failed to read list", err)
	}
	if data == nil {
		return Failure[[]Repository]("LIST_NOT_FOUND", "List '"+listID+"' not found")
	}

	repos := make([]Repository, 0, len(data.Repositories))
	for _, repoRef := range data.Repositories {
		id := protocol.ParseRepositoryID(repoRef)
		url, branch := id.Repository, id.Branch
		repo := Repository{
			ID:     url,
			URL:    url,
			Name:   protocol.GetFullDisplayName(url),
			Branch: branch,
			Type:   RepositoryTypeOther,
			Lists:  []string{listID},
		}
		if ranges, err := cache.GetFetchRanges(url); err == nil && len(ranges) > 0 {
			repo.FetchedRanges = make([]FetchedRange, len(ranges))
			for i, r := range ranges {
				repo.FetchedRanges[i] = FetchedRange{Start: r.RangeStart, End: r.RangeEnd}
			}
		}
		repos = append(repos, repo)
	}

	return Success(repos)
}

// getAllRepositories retrieves all repositories from lists and cache.
func getAllRepositories(workdir string) Result[[]Repository] {
	listsResult := GetLists(workdir)
	if !listsResult.Success {
		return Failure[[]Repository](listsResult.Error.Code, listsResult.Error.Message)
	}

	repoMap := make(map[string]*Repository)
	for _, list := range listsResult.Data {
		for _, repoRef := range list.Repositories {
			id := protocol.ParseRepositoryID(repoRef)
			url, branch := id.Repository, id.Branch
			if existing, ok := repoMap[url]; ok {
				existing.Lists = append(existing.Lists, list.ID)
			} else {
				repoMap[url] = &Repository{
					ID:     url,
					URL:    url,
					Name:   protocol.GetFullDisplayName(url),
					Branch: branch,
					Type:   RepositoryTypeOther,
					Lists:  []string{list.ID},
				}
			}
		}
	}

	cachedRepos, err := cache.GetRepositories()
	if err == nil {
		for _, cached := range cachedRepos {
			if _, ok := repoMap[cached.URL]; !ok {
				repoMap[cached.URL] = &Repository{
					ID:     cached.URL,
					URL:    cached.URL,
					Name:   protocol.GetFullDisplayName(cached.URL),
					Branch: cached.Branch,
					Type:   RepositoryTypeOther,
					Lists:  []string{},
				}
			}
		}
	}

	repos := make([]Repository, 0, len(repoMap))
	for _, repo := range repoMap {
		if ranges, err := cache.GetFetchRanges(repo.URL); err == nil && len(ranges) > 0 {
			repo.FetchedRanges = make([]FetchedRange, len(ranges))
			for i, r := range ranges {
				repo.FetchedRanges[i] = FetchedRange{Start: r.RangeStart, End: r.RangeEnd}
			}
		}
		repos = append(repos, *repo)
	}

	sort.Slice(repos, func(i, j int) bool {
		return repos[i].Name < repos[j].Name
	})

	return Success(repos)
}

// GetRelatedRepositories finds repositories related by shared lists or authors.
func GetRelatedRepositories(workdir, targetURL string) Result[[]RelatedRepository] {
	listsResult := GetLists(workdir)
	if !listsResult.Success {
		return Failure[[]RelatedRepository](listsResult.Error.Code, listsResult.Error.Message)
	}

	targetLists := make(map[string]bool)
	repoToLists := make(map[string][]string)
	for _, list := range listsResult.Data {
		for _, repoRef := range list.Repositories {
			id := protocol.ParseRepositoryID(repoRef)
			if id.Repository == "" {
				continue
			}
			repoToLists[id.Repository] = append(repoToLists[id.Repository], list.ID)
			if id.Repository == targetURL {
				targetLists[list.ID] = true
			}
		}
	}

	items, err := GetSocialItems(SocialQuery{Limit: 1000})
	if err != nil {
		items = []SocialItem{}
	}

	targetAuthors := make(map[string]bool)
	repoToAuthors := make(map[string][]string)
	for _, item := range items {
		repoToAuthors[item.RepoURL] = appendUnique(repoToAuthors[item.RepoURL], item.AuthorEmail)
		if item.RepoURL == targetURL {
			targetAuthors[item.AuthorEmail] = true
		}
	}

	relatedMap := make(map[string]*RelatedRepository)
	for url, lists := range repoToLists {
		if url == targetURL {
			continue
		}
		var sharedLists []string
		for _, listID := range lists {
			if targetLists[listID] {
				sharedLists = append(sharedLists, listID)
			}
		}
		if len(sharedLists) > 0 {
			relatedMap[url] = &RelatedRepository{
				Repository: Repository{
					ID:     url,
					URL:    url,
					Name:   protocol.GetFullDisplayName(url),
					Branch: "main",
					Type:   RepositoryTypeOther,
				},
				Relationships: RelationshipInfo{
					SharedLists: sharedLists,
				},
			}
		}
	}

	for url, authors := range repoToAuthors {
		if url == targetURL {
			continue
		}
		var sharedAuthors []string
		for _, email := range authors {
			if targetAuthors[email] {
				sharedAuthors = append(sharedAuthors, email)
			}
		}
		if len(sharedAuthors) > 0 {
			if existing, ok := relatedMap[url]; ok {
				existing.Relationships.SharedAuthors = sharedAuthors
			} else {
				relatedMap[url] = &RelatedRepository{
					Repository: Repository{
						ID:     url,
						URL:    url,
						Name:   protocol.GetFullDisplayName(url),
						Branch: "main",
						Type:   RepositoryTypeOther,
					},
					Relationships: RelationshipInfo{
						SharedAuthors: sharedAuthors,
					},
				}
			}
		}
	}

	related := make([]RelatedRepository, 0, len(relatedMap))
	for _, r := range relatedMap {
		related = append(related, *r)
	}

	sort.Slice(related, func(i, j int) bool {
		scoreI := len(related[i].Relationships.SharedLists)*2 + len(related[i].Relationships.SharedAuthors)
		scoreJ := len(related[j].Relationships.SharedLists)*2 + len(related[j].Relationships.SharedAuthors)
		return scoreI > scoreJ
	})

	return Success(related)
}

// appendUnique appends an item to a slice only if not already present.
func appendUnique(slice []string, item string) []string {
	for _, s := range slice {
		if s == item {
			return slice
		}
	}
	return append(slice, item)
}
