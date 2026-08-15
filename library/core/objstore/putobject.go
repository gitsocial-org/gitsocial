// putobject.go - upload a single arbitrary object to an s3 remote's bucket.
package objstore

import "net/http"

// PutObjectToRemote uploads data to key (under the remote's prefix) as a plain
// object, overwriting any existing object at that key. It exists for foreign
// root objects the release driver publishes alongside the repo (e.g. the
// install script). contentType sets the object's Content-Type when non-empty;
// the Cache-Control header is applied by the client's per-key policy, so a root
// key such as install.sh is stored no-cache.
func PutObjectToRemote(remoteURL string, env HelperEnv, key string, data []byte, contentType string) error {
	client, prefix, _, err := clientForRemote(remoteURL, env)
	if err != nil {
		return err
	}
	return putObject(client, prefix, key, data, contentType)
}

// putObject uploads data to prefix+key as a plain object on an already-resolved
// client, setting Content-Type when non-empty. The single PUT primitive under
// PutObjectToRemote and the artifact uploads, so callers that upload several
// objects resolve the client once.
func putObject(client *Client, prefix, key string, data []byte, contentType string) error {
	headers := map[string]string{}
	if contentType != "" {
		headers["Content-Type"] = contentType
	}
	resp, err := client.do(http.MethodPut, prefix+key, nil, data, headers)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
