// synced_tip.go - In-memory record of the last tip synced to the cache
package gitmsg

import "sync"

var syncedTipCache sync.Map // "workdir\x00scope" → tip hash

// SyncedTip returns the tip last synced for a workdir scope.
func SyncedTip(workdir, scope string) (string, bool) {
	v, ok := syncedTipCache.Load(workdir + "\x00" + scope)
	if !ok {
		return "", false
	}
	return v.(string), true
}

// SetSyncedTip records the tip last synced for a workdir scope.
func SetSyncedTip(workdir, scope, tip string) {
	syncedTipCache.Store(workdir+"\x00"+scope, tip)
}

// InvalidateSyncedTip drops the recorded tip for a workdir scope.
func InvalidateSyncedTip(workdir, scope string) {
	syncedTipCache.Delete(workdir + "\x00" + scope)
}
