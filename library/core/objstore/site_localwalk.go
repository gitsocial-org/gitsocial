// site_localwalk.go - local-first commit reads for the site items walk.
package objstore

// getCommit returns one commit for the walk, preferring the local odb and falling back to the bucket on a miss; both paths share parseBucketCommit.
func getCommit(src *localCommitSource, client *Client, prefix, sha string) (bucketCommit, error) {
	if body, ok := src.commit(sha); ok {
		return parseBucketCommit(sha, body)
	}
	return getBucketCommit(client, prefix, sha)
}
