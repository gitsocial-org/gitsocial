// pack_seal.go - sealing an already-loose bucket into packfiles, and deleting
// the packed loose copies after a grace period.
//
// A bucket first written by an older binary (or by pushes that all stayed below
// the pack threshold) carries its whole history as loose keys. Sealing packs
// that history, then deletes the loose copies so the bucket stops paying for
// both — the one place the backend deletes an immutable key, and the reason
// deletion waits: an in-flight clone or an open browser session that already
// read objects/info/packs must not 404 mid-walk.
//
// The bucket has several pushers, so nothing here may assume the pushing clone
// matches it. What is safe to pack is derived POSITIVELY from the bucket's refs
// that this clone can actually resolve; a refname it never had contributes
// nothing instead of aborting the pass. The state document is rewritten under
// compare-and-swap, so two pushers finishing at once merge rather than drop each
// other's rounds. Deletion waits on both a push count (observable from the
// bucket alone) and wall time (which a busy bucket cannot burn through).
//
// The pass is self-rate-limited (one bucket listing every packSealInterval
// ref-moving pushes) and entirely best-effort: every step is idempotent, and a
// failure only leaves the bucket as it was.
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
	// packStateKey records the sealing pass's push counter and its pending
	// deletions. Mutable, under the reserved .gitsocial/ namespace.
	packStateKey = ".gitsocial/pack-state.json"
	// packStateVersion is the state document's schema version.
	packStateVersion = 1
	// packSealInterval is how many ref-moving pushes pass between sealing
	// attempts. Each attempt LISTs the bucket's objects/ prefix (one round trip
	// per 1,000 keys), which is far too expensive to repeat every push.
	packSealInterval = 25
	// packSealLooseThreshold seals early once this many objects have gone loose
	// since the last seal. A stock dumb-protocol clone fetches every loose
	// object as its own request, so its cost grows with the loose count, not
	// the push count: 25 small pushes drift a clone from ~2s to ~10s, and one
	// just-under-pack-threshold push can add ~1,000 loose objects alone. The
	// LIST-cost rationale above does not apply here: on a bucket this trigger
	// keeps sealed, the loose set stays a few hundred keys, one round trip.
	packSealLooseThreshold = 64
	// packDeleteGrace is how many further pushes a sealed round's loose objects
	// survive before deletion, so a clone or a browser session that started
	// against the all-loose bucket finishes against it.
	packDeleteGrace = 3
	// packDeleteGraceWindow is the wall-clock floor under the same grace. The
	// push counter alone is not a window on a shared bucket: N pushers burn it N
	// times faster in real time, and a burst of pushes can expire a 3-push grace
	// in seconds, 404-ing a clone that is still walking loose objects. Both the
	// counter and this must clear before a loose copy goes.
	//
	// Advisory, not a guarantee: the sealing clone stamps SealedAt from its own
	// clock and every other pusher compares that stamp against its own, so a
	// machine an hour fast treats a fresh round as expired on sight, and one an
	// hour slow holds a round past the window. A bucket has no shared clock, so
	// the push counter stays the half of the grace nothing can skew and this is
	// the softer half.
	packDeleteGraceWindow = time.Hour
)

// packState is the sealing pass's bucket state: the ref-moving push counter,
// the counter value at the last successful sealing attempt, the rounds whose
// loose objects are waiting out their grace period, and the last seal failure
// (kept on the bucket because a remote helper's stderr scrolls past unread).
type packState struct {
	Version    int `json:"version"`
	Generation int `json:"generation"`
	LastSeal   int `json:"lastSeal"`
	// LooseSinceSeal counts non-state objects pushes have uploaded loose since
	// the last successful seal; crossing packSealLooseThreshold seals early.
	// Additive under the concurrent replay, and a binary predating the field
	// drops it on rewrite, which only defers the early trigger to the
	// packSealInterval backstop.
	LooseSinceSeal int         `json:"looseSinceSeal,omitempty"`
	Pending        []packRound `json:"pending,omitempty"`
	LastError      string      `json:"lastError,omitempty"`
	LastErrorAt    int64       `json:"lastErrorAt,omitempty"`
}

// packRound is one sealing round awaiting deletion: the packs it wrote, the
// generation at which their loose copies may go, and the wall-clock second it
// was sealed at (0 in a round written before the wall-clock floor existed,
// which then clears it immediately).
type packRound struct {
	Packs       []string `json:"packs"`
	DeleteAfter int      `json:"deleteAfter"`
	SealedAt    int64    `json:"sealedAt,omitempty"`
}

// packStateUpdate is one maintenance pass's effect on the sealing state, kept
// apart from the state the pass read so it can be replayed onto whatever a
// concurrent pusher published meanwhile (see commitPackState).
type packStateUpdate struct {
	deleted       map[string]bool // round key → its loose copies are gone
	sealed        *packRound      // the round this pass published, if any
	sealAttempted bool
	sealErr       string // empty when the attempt succeeded
	looseUploaded int    // non-state objects this push uploaded loose
}

// maintainPacks advances the sealing state one push: it counts the push, runs
// any deletion whose grace has expired, and — when a seal is due, see sealDue —
// seals whatever loose history the bucket still carries. Best-effort: a failure
// is reported, recorded on the bucket, and retried by the next push.
func (h *remoteHelper) maintainPacks(refs map[string]string) {
	state, err := readPackState(h.client, h.prefix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: pack state: %v\n", err)
		return
	}
	// An outstanding failure is repeated on EVERY push until a seal succeeds, not
	// only on the push that hit it: the original line scrolls past unread (one
	// rode through a release that way), and the bucket stays unpacked meanwhile.
	if state.LastError != "" {
		since := ""
		if state.LastErrorAt > 0 {
			since = " since " + time.Unix(state.LastErrorAt, 0).UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(os.Stderr, "gitsocial s3: sealing has been failing%s, bucket stays unpacked: %s\n", since, state.LastError)
	}
	// The state read is a snapshot taken before this push was counted, so every
	// decision below is judged against the generation this push takes.
	generation := state.Generation + 1
	update := packStateUpdate{deleted: map[string]bool{}, looseUploaded: h.looseUploaded}
	// objects/info/packs is the only way either reader — stock git's dumb walker
	// and the browser — discovers a pack, and it is rewritten from whole listing
	// snapshots by concurrent pushers, so one pusher's snapshot can drop the line
	// another just published. A round's loose copies therefore go only once a
	// FRESH read of that listing proves its packs are advertised: durable is not
	// enough, an unadvertised pack is a 404 to every reader. Read once per pass,
	// and only when a round is otherwise due.
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
		if err := deleteRoundLooseObjects(h.client, h.prefix, round, resolveUploadConcurrency()); err != nil {
			fmt.Fprintf(os.Stderr, "gitsocial s3: pack deletion: %v\n", err)
			continue
		}
		update.deleted[roundKey(round)] = true
	}
	if why := unsealableClone(); why != "" && sealDue(state, generation, h.looseUploaded) {
		// Leaving sealAttempted false keeps LastSeal where it is, so the next
		// clone with full history still runs the pass on its own push.
		fmt.Fprintf(os.Stderr, "gitsocial s3: skipping the sealing pass (%s); a clone carrying full history will run it\n", why)
	} else if sealDue(state, generation, h.looseUploaded) {
		round, err := h.sealLooseObjects(refs)
		// A round that published packs is recorded even alongside an error: those
		// packs are on the bucket, so their loose copies must still be collected.
		if len(round.Packs) > 0 {
			update.sealed = &round
		}
		update.sealAttempted = true
		if err != nil {
			update.sealErr = oneLine(err)
			fmt.Fprintf(os.Stderr, "gitsocial s3: pack seal FAILED, bucket stays unpacked (retried on the next push): %v\n", err)
		}
	}
	if err := commitPackState(h.client, h.capability, h.prefix, update); err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: pack state: %v\n", err)
	}
}

// sealDue reports whether this push should attempt a sealing pass: never
// sealed, the push-count interval expired, or enough objects have gone loose
// since the last seal that clone latency is drifting (a stock dumb-protocol
// clone pays one request per loose object). The loose count is judged including
// this push's own uploads, the same way generation is judged as the value this
// push takes.
func sealDue(state *packState, generation, looseUploaded int) bool {
	return state.LastSeal == 0 ||
		generation-state.LastSeal >= packSealInterval ||
		state.LooseSinceSeal+looseUploaded >= packSealLooseThreshold
}

// unsealableClone names why the pushing clone must not drive a sealing pass, or
// "" when it may. A partial clone resolves a missing object by lazily fetching
// it from its promisor, so walking the bucket's history here would drag the
// whole repo over the network in the middle of someone's push; a shallow clone
// cannot see the history the bucket carries at all. Either way another clone
// seals on a later push, so skipping costs only a deferred pass.
func unsealableClone() string {
	if out, err := gitOutput("rev-parse", "--is-shallow-repository"); err == nil && out == "true" {
		return "shallow clone"
	}
	if out, err := gitOutput("config", "--get-regexp", `^remote\..*\.promisor$`); err == nil && out != "" {
		return "partial clone"
	}
	return ""
}

// sealLooseObjects packs whatever loose history this clone may seal (see
// sealableObjects) and returns the round holding the packs that landed, so their
// loose copies can be deleted once the grace period expires. The round is
// returned alongside an error too: a pack that was published must still have its
// loose copies collected.
//
// SealedAt is stamped when the pass RETURNS, not when it starts. The first seal
// of a large all-loose history runs for a long time (a full-history
// pack-objects, a multi-GB PUT, up to 256 pack map shards, then the listing), and
// a round stamped at the start hands back a wall-clock grace it has already
// spent — so loose keys would go while a clone that read the pre-seal listing is
// still walking them, exactly what the window exists to prevent. Every return
// path carrying packs is stamped, including the failed ones: those packs are on
// the bucket, so their round is recorded and must carry a real window with it.
func (h *remoteHelper) sealLooseObjects(refs map[string]string) (round packRound, err error) {
	defer func() {
		if len(round.Packs) > 0 {
			round.SealedAt = time.Now().Unix()
		}
	}()
	loose, err := listLooseObjects(h.client, h.prefix)
	if err != nil {
		return round, err
	}
	if len(loose) < resolvePackThreshold() {
		return round, nil
	}
	sealable, err := sealableObjects(loose, refs)
	if err != nil {
		return round, err
	}
	if len(sealable) < resolvePackThreshold() {
		return round, nil
	}
	packs, err := buildDeltaPacks(sealable)
	if err != nil {
		return round, err
	}
	// Only the packs that actually landed enter the round, so a half of the
	// split skipped for size keeps its loose objects (deletion is per round).
	for _, built := range packs {
		if len(built.pack) > maxPackUploadBytes {
			continue
		}
		if err := publishPack(h.client, h.capability, h.prefix, built, resolveUploadConcurrency()); err != nil {
			return round, err
		}
		round.Packs = append(round.Packs, built.name)
	}
	if len(round.Packs) == 0 {
		return round, nil
	}
	// The new packs only become discoverable once they are listed, and the
	// listing is what starts the grace clock for every reader.
	listed, err := listBucketPacks(h.client, h.prefix)
	if err != nil {
		return round, err
	}
	return round, putText(h.client, h.prefix+packsKey, buildInfoPacks(listed))
}

// sealableObjects derives what this clone may pack out of the bucket's loose
// keys, positively: reachable from a bucket ref that is NOT a state ref, minus
// everything a state ref reaches (those stay loose — site maintenance reads them
// as plain keys and has no pack fallback), minus whatever the local odb lacks.
//
// Positive derivation is what makes a bucket ref this clone never had harmless.
// The bucket carries refs from every pusher, so resolving its refnames against
// the local repo is wrong twice over: it fails outright on a refname that is not
// there (which used to abort every seal), and making that failure tolerable
// would instead let a state ref's objects fall out of the exclusion set and be
// packed. Deriving from the tips this clone resolves does neither: an
// unresolvable state ref subtracts nothing AND its objects are unreachable from
// any non-state tip, so they are never sealed in the first place.
func sealableObjects(loose []string, refs map[string]string) ([]string, error) {
	stateTips, contentTips := bucketRefTips(refs)
	packable, err := reachableObjects(contentTips)
	if err != nil {
		return nil, err
	}
	stateShas, err := reachableObjects(stateTips)
	if err != nil {
		return nil, err
	}
	var candidates []string
	for _, sha := range loose {
		if packable[sha] && !stateShas[sha] {
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

// bucketRefTips splits a bucket ref listing into the state-ref tips
// (refs/gitmsg/*, whose objects stay loose) and every other tip, keeping only
// the shas the local odb actually carries. Tips, not refnames: the bucket's
// refs come from every clone that ever pushed, and only their object values
// mean anything in this repo.
func bucketRefTips(refs map[string]string) (stateTips, contentTips []string) {
	for name, sha := range refs {
		if len(sha) != 40 || !isHexString(sha) {
			continue
		}
		if _, err := gitOutput("cat-file", "-e", sha+"^{object}"); err != nil {
			continue
		}
		if strings.HasPrefix(name, "refs/gitmsg/") {
			stateTips = append(stateTips, sha)
		} else {
			contentTips = append(contentTips, sha)
		}
	}
	// Sorted so the rev-list below is a deterministic command for a given bucket.
	sort.Strings(stateTips)
	sort.Strings(contentTips)
	return stateTips, contentTips
}

// reachableObjects returns every object reachable from tips, the tips included:
// `rev-list --objects` peels an annotated tag, so each tip is added back
// explicitly. Empty (and no error) for no tips.
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

// roundDeletable reports whether a sealed round's loose copies may go: its
// push-counted grace has run out AND enough wall time has passed since it was
// sealed. See packDeleteGraceWindow for why one without the other is not a
// window at all.
func roundDeletable(round packRound, generation int) bool {
	return round.DeleteAfter <= generation && time.Since(time.Unix(round.SealedAt, 0)) >= packDeleteGraceWindow
}

// roundKey identifies a pending round by the packs it wrote, so a pass can name
// the rounds it deleted against a state document another pusher meanwhile moved.
func roundKey(round packRound) string { return strings.Join(round.Packs, ",") }

// advertisedPacks reads the pack names objects/info/packs currently lists, the
// bucket's own answer to "which packs can a reader find". An absent listing is
// an empty set, not an error: an all-loose bucket has none.
func advertisedPacks(client *Client, prefix string) (map[string]bool, error) {
	body, err := client.GetRetry(prefix + packsKey)
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

// roundAdvertised reports whether every pack a sealed round wrote is listed in
// objects/info/packs, i.e. whether a reader can still reach the objects whose
// loose copies the round is about to delete.
func roundAdvertised(round packRound, advertised map[string]bool) bool {
	for _, name := range round.Packs {
		if !advertised[name] {
			return false
		}
	}
	return true
}

// applyPackStateUpdate replays one maintenance pass onto a state document: it
// counts the push, drops the rounds whose loose copies the pass deleted, records
// the round it sealed, and stamps the seal outcome. LastSeal advances only on a
// successful attempt, so a failed seal is retried by the next push instead of
// being hidden for another packSealInterval pushes.
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
	// The seal packed the loose set its LIST saw; a concurrent push's objects
	// that landed after that snapshot are undercounted until that pusher's own
	// next push, which the interval backstop covers.
	state.LooseSinceSeal = 0
}

// pendingRound reports whether a state already carries a round writing the same
// packs, so a re-seal of an unchanged loose set does not queue it twice.
func pendingRound(state *packState, key string) bool {
	for _, round := range state.Pending {
		if roundKey(round) == key {
			return true
		}
	}
	return false
}

// deleteRoundLooseObjects removes the loose key of every object a sealed
// round's packs carry, enumerated from the PUBLISHED indexes so a key is only
// ever deleted against a pack the bucket demonstrably has. A pack of the round
// missing from the bucket fails the whole round without deleting anything: the
// round is deleted as one unit, so collecting the packs that are there would
// drop the round from the state while the missing pack's loose copies remain,
// leaking them with nothing left tracking them. Failing keeps the round pending,
// visible, and retried. Deletion is idempotent (S3 DELETE on a missing key
// succeeds), so a retried round is free.
func deleteRoundLooseObjects(client *Client, prefix string, round packRound, concurrency int) error {
	var shas []string
	for _, name := range round.Packs {
		idx, err := client.GetRetry(prefix + packKeyPrefix + name + ".idx")
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

// listLooseObjects returns every sha the bucket stores as a loose key.
func listLooseObjects(client *Client, prefix string) ([]string, error) {
	keys, err := client.List(prefix + "objects/")
	if err != nil {
		return nil, fmt.Errorf("list objects: %w", err)
	}
	var shas []string
	for _, key := range keys {
		rel := strings.TrimPrefix(key, prefix+"objects/")
		rel = strings.Replace(rel, "/", "", 1)
		if len(rel) == 40 && isHexString(rel) {
			shas = append(shas, rel)
		}
	}
	return shas, nil
}

// readPackState fetches the sealing state, returning a zero state when the key
// is absent. A document that IS there but does not parse, or that another schema
// version wrote, is an error rather than a zero state: this pass must neither
// overwrite a document it cannot read (that drops pending rounds, stranding
// their loose copies beside their packs forever) nor run blind against one,
// which would re-seal the whole bucket on every push.
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

// commitPackState applies one maintenance pass to the bucket's sealing state
// under compare-and-swap, so two pushers finishing at once merge instead of
// overwriting: the loser re-reads and replays its own pass on top. A plain write
// here loses whole pending rounds, and a dropped round is a pair of loose and
// packed copies nothing ever collects. A document another schema version wrote
// is left alone for the same reason: found=false means the key is absent (the
// merge then starts from the zero document), never that a document is there but
// unreadable.
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

// writePackState publishes the sealing state as-is, overwriting whatever the
// bucket holds. Only for a caller that owns the whole document; every
// maintenance pass goes through commitPackState instead.
func writePackState(client *Client, prefix string, state *packState) error {
	state.Version = packStateVersion
	compressed, err := compressJSON(state, brotliQualityFull)
	if err != nil {
		return err
	}
	return putCompressed(client, prefix+packStateKey, compressed)
}
