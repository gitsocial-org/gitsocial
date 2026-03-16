// methods_release.go - Release extension RPC methods: releases, comments
package rpc

import (
	"encoding/json"

	"github.com/gitsocial-org/gitsocial/extensions/release"
)

// RegisterReleaseMethods registers release.* methods on the server.
func RegisterReleaseMethods(s *Server) {
	s.registry.Register("release.getReleases", s.requireInit(releaseGetReleases(s)))
	s.registry.Register("release.getRelease", s.requireInit(releaseGetRelease()))
	s.registry.Register("release.createRelease", s.requireInit(releaseCreateRelease(s)))
	s.registry.Register("release.editRelease", s.requireInit(releaseEditRelease(s)))
	s.registry.Register("release.retractRelease", s.requireInit(releaseRetractRelease(s)))
	s.registry.Register("release.getReleaseComments", s.requireInit(releaseGetReleaseComments(s)))
	s.registry.Register("release.getSBOM", s.requireInit(releaseGetSBOM(s)))
	s.registry.Register("release.getSBOMRaw", s.requireInit(releaseGetSBOMRaw(s)))
}

func releaseGetReleases(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			RepoURL string `json:"repoURL"`
			Branch  string `json:"branch"`
			Limit   int    `json:"limit"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		repoURL := p.RepoURL
		if repoURL == "" {
			repoURL = s.session.RepoURL
		}
		return fromResult(release.GetReleases(repoURL, p.Branch, "", p.Limit))
	}
}

func releaseGetRelease() HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Ref string `json:"ref"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Ref == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "ref is required"}
		}
		return fromResult(release.GetSingleRelease(p.Ref))
	}
}

func releaseCreateRelease(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Subject     string   `json:"subject"`
			Body        string   `json:"body"`
			Tag         string   `json:"tag"`
			Version     string   `json:"version"`
			Prerelease  bool     `json:"prerelease"`
			Artifacts   []string `json:"artifacts"`
			ArtifactURL string   `json:"artifactURL"`
			Checksums   string   `json:"checksums"`
			SignedBy    string   `json:"signedBy"`
			SBOM        string   `json:"sbom"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Subject == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "subject is required"}
		}
		return fromResult(release.CreateRelease(s.session.Workdir, p.Subject, p.Body, release.CreateReleaseOptions{
			Tag:         p.Tag,
			Version:     p.Version,
			Prerelease:  p.Prerelease,
			Artifacts:   p.Artifacts,
			ArtifactURL: p.ArtifactURL,
			Checksums:   p.Checksums,
			SignedBy:    p.SignedBy,
			SBOM:        p.SBOM,
		}))
	}
}

func releaseEditRelease(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Ref         string    `json:"ref"`
			Subject     *string   `json:"subject"`
			Body        *string   `json:"body"`
			Tag         *string   `json:"tag"`
			Version     *string   `json:"version"`
			Prerelease  *bool     `json:"prerelease"`
			Artifacts   *[]string `json:"artifacts"`
			ArtifactURL *string   `json:"artifactURL"`
			Checksums   *string   `json:"checksums"`
			SignedBy    *string   `json:"signedBy"`
			SBOM        *string   `json:"sbom"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Ref == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "ref is required"}
		}
		return fromResult(release.EditRelease(s.session.Workdir, p.Ref, release.EditReleaseOptions{
			Subject:     p.Subject,
			Body:        p.Body,
			Tag:         p.Tag,
			Version:     p.Version,
			Prerelease:  p.Prerelease,
			Artifacts:   p.Artifacts,
			ArtifactURL: p.ArtifactURL,
			Checksums:   p.Checksums,
			SignedBy:    p.SignedBy,
			SBOM:        p.SBOM,
		}))
	}
}

func releaseRetractRelease(s *Server) HandlerFunc {
	return refAction(func(workdir, ref string) (any, *RPCError) {
		return fromResult(release.RetractRelease(workdir, ref))
	}, s)
}

func releaseGetSBOM(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Ref string `json:"ref"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Ref == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "ref is required"}
		}
		return fromResult(release.GetSBOMDetails(s.session.Workdir, p.Ref))
	}
}

func releaseGetSBOMRaw(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Ref string `json:"ref"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Ref == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "ref is required"}
		}
		res := release.GetSingleRelease(p.Ref)
		if !res.Success {
			return nil, &RPCError{Code: appErrorCode(res.Error.Code), Message: res.Error.Message}
		}
		rel := res.Data
		if rel.SBOM == "" || rel.Version == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "release has no SBOM or version"}
		}
		return fromResult(release.GetSBOMRaw(s.session.Workdir, rel.Version, rel.SBOM))
	}
}

func releaseGetReleaseComments(s *Server) HandlerFunc {
	return func(raw json.RawMessage) (any, *RPCError) {
		p, rpcErr := decodeParams[struct {
			Ref string `json:"ref"`
		}](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if p.Ref == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "ref is required"}
		}
		return fromResult(release.GetReleaseComments(p.Ref, s.session.RepoURL))
	}
}
