// site_localwalk.go - local-first commit reads for the site items walk.
package objstore

// getCommit returns one commit for the walk, preferring the local odb and
// falling back to the bucket GET on a local miss. src may be nil (bucket-only).
// The bucket parse and the local parse share parseBucketCommit, so both paths
// yield an identical bucketCommit for the same sha.
func getCommit(src *localCommitSource, client *Client, prefix, sha string) (bucketCommit, error) {
	if body, ok := src.commit(sha); ok {
		return parseBucketCommit(sha, body)
	}
	return getBucketCommit(client, prefix, sha)
}
