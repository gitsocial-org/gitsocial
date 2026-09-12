// pack_seal.go - sealing an already-loose bucket into packfiles, and collecting the loose copies after a grace period
package objstore

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	// packStateKey records the sealing pass's push counter and its pending deletions.
	packStateKey = ".gitsocial/pack-state.json"
	// packStateVersion is the state document's schema version.
	packStateVersion = 1
	// packSealInterval is how many ref-moving pushes pass between sealing attempts, each of which lists objects/.
	packSealInterval = 25
	// packSealLooseThreshold seals early once this many objects have gone loose, since a dumb clone pays one request per loose object.
	packSealLooseThreshold = 64
	// packDeleteGrace is how many further pushes a sealed round's loose objects survive.
	packDeleteGrace = 3
	// packDeleteGraceWindow is the wall-clock floor under the same grace; it is advisory, since a bucket has no shared clock.
	packDeleteGraceWindow = time.Hour
)

// packState is the sealing pass's bucket state: the push counter, the last attempt, the rounds awaiting deletion, and the last failure.
type packState struct {
	Version    int `json:"version"`
	Generation int `json:"generation"`
	// LastSeal is the counter at the last sealing attempt; a decline advances it, a failure does not.
	LastSeal int `json:"lastSeal"`
	// LooseSinceSeal counts objects uploaded loose since the last attempt; crossing packSealLooseThreshold seals early.
	LooseSinceSeal int         `json:"looseSinceSeal,omitempty"`
	Pending        []packRound `json:"pending,omitempty"`
	LastError      string      `json:"lastError,omitempty"`
	LastErrorAt    int64       `json:"lastErrorAt,omitempty"`
}

// packRound is one sealing round awaiting deletion: the packs it wrote, the generation their loose copies may go at, and the second it was sealed.
type packRound struct {
	Packs       []string `json:"packs"`
	DeleteAfter int      `json:"deleteAfter"`
	SealedAt    int64    `json:"sealedAt,omitempty"`
}

// packStateUpdate is one pass's effect on the sealing state, kept apart so it can be replayed onto what a concurrent pusher published.
type packStateUpdate struct {
	deleted       map[string]bool // round key → its loose copies are gone
	sealed        *packRound      // the round this pass published, if any
	sealAttempted bool
	sealErr       string // empty when the attempt succeeded
	looseUploaded int    // objects this push uploaded loose
	// looseAfterSeal is what the attempt's sealable set left unpacked; negative means no measurement, so the counting stays additive.
	looseAfterSeal int
}

// maintainPacks advances the sealing state one push: count it, run any expired deletion, and seal when sealDue says so.
func (h *remoteHelper) maintainPacks(refs map[string]string) {
	state, err := readPackState(h.client, h.prefix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: pack state: %v\n", err)
		return
	}
	// Repeat an outstanding failure on every push until a seal succeeds; one line scrolls past unread.
	if state.LastError != "" {
		since := ""
		if state.LastErrorAt > 0 {
			since = " since " + time.Unix(state.LastErrorAt, 0).UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(os.Stderr, "gitsocial s3: sealing has been failing%s, bucket stays unpacked: %s\n", since, state.LastError)
	}
	// The state read predates this push's count, so every decision below is judged against the generation it takes.
	generation := state.Generation + 1
	update := packStateUpdate{deleted: map[string]bool{}, looseUploaded: h.looseUploaded, looseAfterSeal: -1}
	// A round's loose copies go only once a fresh read of objects/info/packs proves its packs are listed there; an unadvertised pack is a 404 to every reader.
	var advertised map[string]bool
	for _, round := range state.Pending {
		if !roundDeletable(round, generation) {
			continue
		}
		if advertised == nil {
			names, err := advertisedPacks(h.client, h.prefix)
			if err != nil {
				fmt.Fprintf(os.Stderr, "gitsocial s3: pack listing: %v\n", err)
				break // can't prove discoverability: every loose copy stays
			}
			advertised = names
		}
		if !roundAdvertised(round, advertised) {
			fmt.Fprintf(os.Stderr, "gitsocial s3: %s does not list the packs of round %s yet; keeping their loose objects\n", packsKey, roundKey(round))
			continue
		}
		if err := deleteRoundLooseObjects(h.client, h.prefix, round, UploadConcurrency()); err != nil {
			fmt.Fprintf(os.Stderr, "gitsocial s3: pack deletion: %v\n", err)
			continue
		}
		update.deleted[roundKey(round)] = true
	}
	due := sealDue(state, generation, h.looseUploaded)
	if why := unsealableClone(); why != "" && due {
		// Leaving sealAttempted false keeps LastSeal put, so the next clone with full history runs the pass.
		fmt.Fprintf(os.Stderr, "gitsocial s3: skipping the sealing pass (%s); a clone carrying full history will run it\n", why)
	} else if due {
		round, looseAfter, err := h.sealLooseObjects(refs)
		// A round that published packs is recorded even alongside an error, since its loose copies still need collecting.
		if len(round.Packs) > 0 {
			update.sealed = &round
		}
		update.sealAttempted = true
		update.looseAfterSeal = looseAfter
		if err != nil {
			update.sealErr = oneLine(err)
			fmt.Fprintf(os.Stderr, "gitsocial s3: pack seal FAILED, bucket stays unpacked (retried on the next push): %v\n", err)
		}
	}
	if err := commitPackState(h.client, h.capability, h.prefix, update); err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: pack state: %v\n", err)
	}
}

// sealDue reports whether this push should attempt a seal: no seal yet, the interval expired, or enough objects gone loose since the last one.
func sealDue(state *packState, generation, looseUploaded int) bool {
	return state.LastSeal == 0 ||
		generation-state.LastSeal >= packSealInterval ||
		state.LooseSinceSeal+looseUploaded >= packSealLooseThreshold
}

// unsealableClone names why the pushing clone must not drive a sealing pass: a partial clone would drag its promisor, a shallow one cannot see the history.
func unsealableClone() string {
	if out, err := gitOutput("rev-parse", "--is-shallow-repository"); err == nil && out == "true" {
		return "shallow clone"
	}
	if out, err := gitOutput("config", "--get-regexp", `^remote\..*\.promisor$`); err == nil && out != "" {
		return "partial clone"
	}
	return ""
}

// resolveSealThreshold returns the minimum object count a seal packs: the drift trigger itself, so a fired trigger cannot immediately decline.
func resolveSealThreshold() int {
	return envInt("GITSOCIAL_S3_PACK_THRESHOLD", packSealLooseThreshold)
}

// sealLooseObjects packs whatever loose history this clone may seal and returns the round the packs landed in, error or not. SealedAt is stamped on return, not entry, so a long first seal does not hand back a spent grace.
func (h *remoteHelper) sealLooseObjects(refs map[string]string) (round packRound, looseAfter int, err error) {
	looseAfter = -1
	defer func() {
		if len(round.Packs) > 0 {
			round.SealedAt = time.Now().Unix()
		}
	}()
	loose, err := listLooseObjects(h.client, h.prefix)
	if err != nil {
		return round, looseAfter, err
	}
	if len(loose) < resolveSealThreshold() {
		return round, len(loose), nil
	}
	sealable, err := sealableObjects(h.localOdb(), loose, refs)
	if err != nil {
		return round, looseAfter, err
	}
	if len(sealable) < resolveSealThreshold() {
		return round, len(sealable), nil
	}
	packs, err := buildDeltaPacks(sealable)
	if err != nil {
		return round, looseAfter, err
	}
	// Only the packs that landed enter the round, so a half skipped for size keeps its loose objects.
	published := 0
	for _, built := range packs {
		if len(built.pack) > maxPackUploadBytes {
			continue
		}
		if err := publishPack(h.client, h.capability, h.prefix, built, UploadConcurrency()); err != nil {
			return round, looseAfter, err
		}
		round.Packs = append(round.Packs, built.name)
		published += built.objects
	}
	looseAfter = len(sealable) - published
	if len(round.Packs) == 0 {
		return round, looseAfter, nil
	}
	// New packs become discoverable only once listed, and that listing starts every reader's grace clock.
	listed, err := listBucketPacks(h.client, h.prefix)
	if err != nil {
		return round, looseAfter, err
	}
	return round, looseAfter, putText(h.client, h.prefix+packsKey, buildInfoPacks(listed))
}

// sealableObjects derives what this clone may pack from the bucket's loose keys: reachable from a resolvable bucket ref tip, minus what the local odb lacks.
func sealableObjects(src *LocalCommitSource, loose []string, refs map[string]string) ([]string, error) {
	packable, err := reachableObjects(bucketRefTips(src, refs))
	if err != nil {
		return nil, err
	}
	var candidates []string
	for _, sha := range loose {
		if packable[sha] {
			candidates = append(candidates, sha)
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	commitLike, content, _, err := classifyObjects(candidates)
	if err != nil {
		return nil, err
	}
	return append(commitLike, content...), nil
}

// bucketRefTips returns every bucket ref tip whose object the local odb carries, through one batch call; only the values mean anything in this repo, not the refnames.
func bucketRefTips(src *LocalCommitSource, refs map[string]string) []string {
	wellFormed := map[string]string{}
	for name, sha := range refs {
		if len(sha) == 40 && isHexString(sha) {
			wellFormed[name] = sha
		}
	}
	return presentLocally(src, wellFormed)
}

// reachableObjects returns every object reachable from tips, the tips included, since rev-list peels an annotated tag.
func reachableObjects(tips []string) (map[string]bool, error) {
	shas := map[string]bool{}
	if len(tips) == 0 {
		return shas, nil
	}
	out, err := gitOutput(append([]string{"rev-list", "--objects"}, tips...)...)
	if err != nil {
		return nil, fmt.Errorf("rev-list bucket tips: %w", err)
	}
	for _, line := range strings.Split(out, "\n") {
		if len(line) >= 40 {
			shas[line[:40]] = true
		}
	}
	for _, tip := range tips {
		shas[tip] = true
	}
	return shas, nil
}

// roundDeletable reports whether a sealed round's loose copies may go: both the push-counted grace and the wall-clock window have cleared.
func roundDeletable(round packRound, generation int) bool {
	return round.DeleteAfter <= generation && time.Since(time.Unix(round.SealedAt, 0)) >= packDeleteGraceWindow
}

// roundKey identifies a pending round by the packs it wrote, so a pass can name its deletions against a moved state document.
func roundKey(round packRound) string { return strings.Join(round.Packs, ",") }

// advertisedPacks reads the pack names objects/info/packs lists; an absent listing is an empty set, not an error.
func advertisedPacks(client *Client, prefix string) (map[string]bool, error) {
	body, err := client.Get(prefix + packsKey)
	if errors.Is(err, ErrNotFound) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", packsKey, err)
	}
	names := map[string]bool{}
	for _, name := range parseInfoPacks(body) {
		names[name] = true
	}
	return names, nil
}

// roundAdvertised reports whether every pack a round wrote is listed in objects/info/packs.
func roundAdvertised(round packRound, advertised map[string]bool) bool {
	for _, name := range round.Packs {
		if !advertised[name] {
			return false
		}
	}
	return true
}

// applyPackStateUpdate replays one pass onto a state document; LastSeal advances on any attempt that reached a decision, but not on a failed one.
func applyPackStateUpdate(state *packState, update packStateUpdate) {
	state.Generation++
	state.LooseSinceSeal += update.looseUploaded
	var keep []packRound
	for _, round := range state.Pending {
		if !update.deleted[roundKey(round)] {
			keep = append(keep, round)
		}
	}
	state.Pending = keep
	if update.sealed != nil && !pendingRound(state, roundKey(*update.sealed)) {
		round := *update.sealed
		round.DeleteAfter = state.Generation + packDeleteGrace
		state.Pending = append(state.Pending, round)
	}
	if !update.sealAttempted {
		return
	}
	state.LastError, state.LastErrorAt = update.sealErr, 0
	if update.sealErr != "" {
		state.LastErrorAt = time.Now().Unix()
		return
	}
	state.LastSeal = state.Generation
	// The attempt measured what its own listing left unpacked, so set the counter to that; the interval backstop covers what a concurrent pusher's increment loses.
	if update.looseAfterSeal >= 0 {
		state.LooseSinceSeal = update.looseAfterSeal
	}
}

// pendingRound reports whether a state already carries a round writing the same packs.
func pendingRound(state *packState, key string) bool {
	for _, round := range state.Pending {
		if roundKey(round) == key {
			return true
		}
	}
	return false
}

// deleteRoundLooseObjects removes the loose key of every object a round's packs carry, enumerated from the published indexes; a missing pack fails the whole round.
func deleteRoundLooseObjects(client *Client, prefix string, round packRound, concurrency int) error {
	var shas []string
	for _, name := range round.Packs {
		idx, err := client.Get(prefix + packKeyPrefix + name + ".idx")
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("pack %s: index absent from the bucket, so nothing of this round is safe to delete", name)
		}
		if err != nil {
			return fmt.Errorf("read pack index %s: %w", name, err)
		}
		entries, err := parsePackIdx(idx)
		if err != nil {
			return fmt.Errorf("pack %s: %w", name, err)
		}
		for _, entry := range entries {
			shas = append(shas, entry.sha)
		}
	}
	return forEachBounded(len(shas), concurrency, func(i int) error {
		sha := shas[i]
		return client.Delete(prefix + "objects/" + sha[:2] + "/" + sha[2:])
	})
}

// bucketLooseObjects returns the sha of every loose object key the bucket carries; the one listing of objects/ the push, the seal and the thin inventory all read.
func bucketLooseObjects(client *Client, prefix string) (map[string]bool, error) {
	keys, err := client.List(prefix + "objects/")
	if err != nil {
		return nil, fmt.Errorf("list objects: %w", err)
	}
	shas := make(map[string]bool, len(keys))
	for _, key := range keys {
		// objects/<xx>/<38-hex>: reassemble the 40-hex sha.
		rel := strings.TrimPrefix(key, prefix+"objects/")
		rel = strings.Replace(rel, "/", "", 1)
		if len(rel) == 40 && isHexString(rel) {
			shas[rel] = true
		}
	}
	return shas, nil
}

// listLooseObjects is bucketLooseObjects as a sorted slice, so a seal packs the same set in the same order every run.
func listLooseObjects(client *Client, prefix string) ([]string, error) {
	present, err := bucketLooseObjects(client, prefix)
	if err != nil {
		return nil, err
	}
	shas := make([]string, 0, len(present))
	for sha := range present {
		shas = append(shas, sha)
	}
	sort.Strings(shas)
	return shas, nil
}

// readPackState fetches the sealing state, zero when absent; a present document it cannot read is an error, since overwriting one strands its pending rounds.
func readPackState(client *Client, prefix string) (*packState, error) {
	var state packState
	found, _, err := readCompressedJSONWithETag(client, prefix+packStateKey, &state)
	if err != nil {
		return nil, err
	}
	if !found {
		return &packState{Version: packStateVersion}, nil
	}
	if state.Version != packStateVersion {
		return nil, fmt.Errorf("%s: schema version %d, this binary writes %d", packStateKey, state.Version, packStateVersion)
	}
	return &state, nil
}

// commitPackState applies one pass to the sealing state under compare-and-swap, so two pushers finishing at once merge instead of dropping each other's rounds.
func commitPackState(client *Client, capability Capability, prefix string, update packStateUpdate) error {
	return updateCompressedJSON(client, capability, prefix+packStateKey, func(state *packState, found bool) error {
		if found && state.Version != packStateVersion {
			return fmt.Errorf("%s: schema version %d, this binary writes %d", packStateKey, state.Version, packStateVersion)
		}
		state.Version = packStateVersion
		applyPackStateUpdate(state, update)
		return nil
	})
}

// writePackState publishes the sealing state as is; only for a caller that owns the whole document, since a pass uses commitPackState.
func writePackState(client *Client, prefix string, state *packState) error {
	state.Version = packStateVersion
	compressed, err := CompressJSON(state, BrotliQualityFull)
	if err != nil {
		return err
	}
	return PutCompressed(client, prefix+packStateKey, compressed, "")
}
