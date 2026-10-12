// site_pages_diff.go - the Files changed section of a pull request page, read from the local object database

package site

import (
	"bufio"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/objstore"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

const (
	// sitePRDiffLineCap bounds the hunk rows one pull request page carries; the app shows the rest.
	sitePRDiffLineCap = 500
	// sitePRDiffFileCap is the app's DIFF_FILE_CAP, the files one Files changed section lists.
	sitePRDiffFileCap = 100
	// siteMergedHeadRefPrefix is review.MergedHeadRefPrefix, which core/site cannot import.
	siteMergedHeadRefPrefix = "refs/gitmsg/review/merged/"
	// sitePRDiffHead is the section's subject on both renderers.
	sitePRDiffHead = "Files changed"
)

// The notices are the app's own sentences for the same cases (PR_DIFF_NOTICE in gs-render.js).
const (
	sitePRDiffNoticeNoTips          = "This pull request records no base or head tip, so no diff can be shown here."
	sitePRDiffNoticeMergeUnresolved = "The recorded merge range does not resolve in this bucket, so no diff can be shown here."
	sitePRDiffNoticeTipsUnresolved  = "The base or head tips are not present in this bucket (a foreign fork, an unfetched branch or a version rebased away), so no diff can be shown here."
)

// sitePageDiff is a pull request page's Files changed section: its files with their hunks, or a notice in their place.
type sitePageDiff struct {
	Count   int
	Notice  string
	Files   []sitePageDiffFile
	Omitted int    // the files past sitePRDiffFileCap
	More    bool   // the rows past sitePRDiffLineCap are in the app
	AppHref string // the app route of the pull request
}

// sitePageDiffFile is one changed file: its status, path, line counts and the hunks the page carries.
type sitePageDiffFile struct {
	Status string // added, deleted or modified
	Letter string // A, D or M
	Path   string
	Adds   string
	Dels   string
	Binary bool
	Hunks  []sitePageDiffHunk
}

// sitePageDiffHunk is one hunk: its @@ head in the app's form and its rows.
type sitePageDiffHunk struct {
	Head  string
	Lines []sitePageDiffLine
}

// sitePageDiffLine is one unified row: its op class, its line numbers, its sign and its text.
type sitePageDiffLine struct {
	Op   string // add, del or ctx
	OldN string
	NewN string
	Sign string
	Text string
}

// siteDiffRevRe admits the revision arguments a header field may hand to git: a sha prefix, or two joined by three dots.
var siteDiffRevRe = regexp.MustCompile(`^[0-9a-f]{7,40}(\.\.\.[0-9a-f]{7,40})?$`)

// siteHunkHeadRe captures the four numbers of a @@ hunk head; an omitted count is 1.
var siteHunkHeadRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// buildSitePRDiff assembles a pull request page's Files changed section: the merge range of a merged pull request first, then the tips of its latest version; nil on any other item.
func buildSitePRDiff(it *sitePageItem, site sitePageSite, src *objstore.LocalCommitSource, refs map[string]string) *sitePageDiff {
	if pageItemType(it) != "pull-request" || it.Retracted {
		return nil
	}
	href := sitePageAppURL(site, "commit:"+it.Msg.Short+"@gitmsg/review")
	baseTip, headTip := pageItemField(it, "base-tip"), pageItemField(it, "head-tip")
	if mergeBase, mergeHead := sitePRMergeRange(it, refs); mergeBase != "" {
		if d, ok := readSitePRDiff(src, href, mergeBase, mergeHead); ok {
			return &d
		}
		if baseTip == "" || headTip == "" {
			return &sitePageDiff{Notice: sitePRDiffNoticeMergeUnresolved}
		}
	}
	if baseTip == "" || headTip == "" {
		return &sitePageDiff{Notice: sitePRDiffNoticeNoTips}
	}
	if sitePRBranchesLocal(it) {
		if d, ok := readSitePRDiff(src, href, baseTip+"..."+headTip); ok {
			return &d
		}
	}
	return &sitePageDiff{Notice: sitePRDiffNoticeTipsUnresolved}
}

// sitePRMergeRange returns a merged pull request's recorded range; the merge head is the kept ref's sha when that sha starts with the recorded short. Both are "" when none is recorded.
func sitePRMergeRange(it *sitePageItem, refs map[string]string) (base, head string) {
	if pageItemField(it, "state") != "merged" {
		return "", ""
	}
	base, head = pageItemField(it, "merge-base"), pageItemField(it, "merge-head")
	if base == "" || head == "" {
		return "", ""
	}
	if kept := refs[siteMergedHeadRefPrefix+it.Msg.Short]; len(kept) == 40 && strings.HasPrefix(kept, head) {
		head = kept
	}
	return base, head
}

// sitePRBranchesLocal reports whether both branch fields name a branch of this repository, as the app's resolveTipCommit requires.
func sitePRBranchesLocal(it *sitePageItem) bool {
	for _, key := range []string{"base", "head"} {
		p := protocol.ParseRef(pageItemField(it, key))
		if p.Repository != "" || p.Type != protocol.RefTypeBranch || p.Value == "" {
			return false
		}
	}
	return true
}

// readSitePRDiff runs one git diff over revs and parses it up to the caps; ok is false when a revision is malformed, there is no local source, or git fails.
func readSitePRDiff(src *objstore.LocalCommitSource, href string, revs ...string) (sitePageDiff, bool) {
	if src == nil {
		return sitePageDiff{}, false
	}
	for _, rev := range revs {
		if !siteDiffRevRe.MatchString(rev) {
			return sitePageDiff{}, false
		}
	}
	args := append([]string{"diff", "--no-color", "--no-ext-diff", "--no-renames", "-z", "--raw", "--numstat", "-p", "-U3"}, revs...)
	cmd := siteGitCommand(src, args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return sitePageDiff{}, false
	}
	if err := cmd.Start(); err != nil {
		return sitePageDiff{}, false
	}
	d, complete := parseSitePRDiff(bufio.NewReaderSize(out, 64<<10))
	d.AppHref = href
	if !complete {
		_ = cmd.Process.Kill() // a cap was reached, so the rest of the output is not read
	}
	err = cmd.Wait()
	return d, !complete || err == nil
}

// parseSitePRDiff reads the raw records, the numstat records and the patch, in the order git -z writes them, and reports whether it reached the end.
func parseSitePRDiff(r *bufio.Reader) (sitePageDiff, bool) {
	var files []sitePageDiffFile
	for {
		b, err := r.Peek(1)
		if err != nil || b[0] != ':' {
			break
		}
		meta, err := r.ReadString(0)
		path, pathErr := r.ReadString(0)
		if err != nil || pathErr != nil {
			break
		}
		fields := strings.Fields(strings.TrimSuffix(meta, "\x00"))
		status := "modified"
		if len(fields) == 5 {
			status = map[string]string{"A": "added", "D": "deleted"}[fields[4]]
			if status == "" {
				status = "modified"
			}
		}
		files = append(files, sitePageDiffFile{Status: status, Letter: strings.ToUpper(status[:1]), Path: strings.TrimSuffix(path, "\x00")})
	}
	// The numstat records follow the raw order, and an empty record closes them.
	for i := 0; ; i++ {
		record, err := r.ReadString(0)
		if err != nil || record == "\x00" {
			break
		}
		if fields := strings.SplitN(strings.TrimSuffix(record, "\x00"), "\t", 3); len(fields) == 3 && i < len(files) {
			files[i].Adds, files[i].Dels, files[i].Binary = fields[0], fields[1], fields[0] == "-"
		}
	}
	d := sitePageDiff{Count: len(files), Files: files}
	if len(files) > sitePRDiffFileCap {
		d.Files, d.Omitted = files[:sitePRDiffFileCap], len(files)-sitePRDiffFileCap
	}
	capped, complete := parseSitePRPatch(r, d.Files)
	d.More = capped
	return d, complete
}

// parseSitePRPatch reads the unified patch into the files by block order; capped reports a stop at the row cap, complete a read to the end, and neither a stop at the last listed file.
func parseSitePRPatch(r *bufio.Reader, files []sitePageDiffFile) (capped, complete bool) {
	file, oldN, newN, rows := -1, 0, 0, 0
	inHunk := false
	for {
		line, err := readSiteDiffLine(r)
		if err != nil {
			return false, err == io.EOF
		}
		switch {
		case strings.HasPrefix(line, "diff --git "):
			file++
			inHunk = false
			if file >= len(files) {
				return false, false
			}
		case strings.HasPrefix(line, "@@"):
			head, o, n, ok := siteHunkHead(line)
			if !ok || file < 0 {
				continue
			}
			files[file].Hunks = append(files[file].Hunks, sitePageDiffHunk{Head: head})
			oldN, newN, inHunk = o, n, true
		case inHunk && line != "" && strings.ContainsRune("+- ", rune(line[0])):
			if rows == sitePRDiffLineCap {
				return true, false
			}
			rows++
			row := sitePageDiffLine{Sign: line[:1], Text: strings.ToValidUTF8(line[1:], "�")}
			switch line[0] {
			case '+':
				row.Op, row.NewN = "add", strconv.Itoa(newN)
				newN++
			case '-':
				row.Op, row.OldN = "del", strconv.Itoa(oldN)
				oldN++
			default:
				row.Op, row.OldN, row.NewN = "ctx", strconv.Itoa(oldN), strconv.Itoa(newN)
				oldN++
				newN++
			}
			hunks := files[file].Hunks
			hunks[len(hunks)-1].Lines = append(hunks[len(hunks)-1].Lines, row)
		}
	}
}

// readSiteDiffLine reads one patch line, cut at the reader's buffer when it is longer.
func readSiteDiffLine(r *bufio.Reader) (string, error) {
	line, isPrefix, err := r.ReadLine()
	if err != nil {
		return "", err
	}
	text := string(line)
	for isPrefix && err == nil {
		_, isPrefix, err = r.ReadLine()
	}
	return text, nil
}

// siteHunkHead reformats a @@ head as the app's hunkHeadText does, with each count written, and returns its two start lines.
func siteHunkHead(line string) (head string, oldStart, newStart int, ok bool) {
	m := siteHunkHeadRe.FindStringSubmatch(line)
	if m == nil {
		return "", 0, 0, false
	}
	n := func(s string) int {
		if s == "" {
			return 1
		}
		v, _ := strconv.Atoi(s) // the pattern admits digits only
		return v
	}
	oldStart, newStart = n(m[1]), n(m[3])
	return "@@ -" + strconv.Itoa(oldStart) + "," + strconv.Itoa(n(m[2])) + " +" + strconv.Itoa(newStart) + "," + strconv.Itoa(n(m[4])) + " @@", oldStart, newStart, true
}
