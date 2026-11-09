// unit_notify_filter.js - the site filters new items to the reader's email when one is set, else keeps all, and caps the notifications at three
require("./shim.js");
const GS = require("../assets/gs-render.js");
let pass = 0, fail = 0;
function eq(a, b, msg) { if (JSON.stringify(a) === JSON.stringify(b)) { pass++; } else { fail++; console.log("FAIL", msg, "got", JSON.stringify(a), "want", JSON.stringify(b)); } }

const ME = "me@example.com";

// item builds an item of an extension with its author, header and body.
function item(short, ext, email, header, content) {
  return { commit: { short, hash: (short + "0".repeat(40)).slice(0, 40), authorTime: 100, authorEmail: email }, header: header || {}, content: content || "", author: email === ME ? "Me" : "Alice", _ext: ext };
}

const myPost = item("a00000000001", "social", ME, {}, "My post");
const mention = item("a00000000002", "social", "alice@example.com", {}, "Hello @ME@Example.com, look");
const assigned = item("a00000000003", "pm", "alice@example.com", { type: "issue", assignees: "bob@example.com, me@example.com" }, "Fix it");
const review = item("a00000000004", "review", "alice@example.com", { type: "pull-request", reviewers: "me@example.com" }, "Add it");
const reply = item("a00000000005", "social", "alice@example.com", { type: "comment", original: "#commit:a00000000001@gitmsg/social" }, "Nice");
const other = item("a00000000006", "social", "alice@example.com", {}, "Mentions @bob@example.com");
const otherReply = item("a00000000007", "social", "alice@example.com", { type: "comment", original: "#commit:a00000000006@gitmsg/social" }, "Agreed");
const own = item("a00000000008", "pm", ME, { type: "issue", assignees: ME }, "Note to @me@example.com");
const fresh = [mention, assigned, review, reply, other, otherReply, own];
const all = fresh.concat([myPost]);

const picked = (email) => GS.pickNotifyItems(fresh, all, email).map((p) => p.item.commit.short + " " + p.type);
eq(picked(ME), ["a00000000002 mention", "a00000000003 issue-assigned", "a00000000004 review-requested", "a00000000005 comment"],
  "with an email the mention, assignee, reviewer and reply to the reader's post pass, the others drop");
eq(picked(" ME@example.com "), picked(ME), "the email matches without case or spaces");
eq(picked(ME).some((p) => p.startsWith("a00000000008")), false, "the reader's own item drops");
eq(picked(""), fresh.map((it) => it.commit.short + " "), "without an email every new item passes");

const three = GS.notifyMessages(GS.pickNotifyItems(fresh.slice(0, 3), all, ME), "GitSocial", "owner/repo");
eq(three.map((m) => [m.title, m.body, m.route]), [
  ["GitSocial", "Alice mentioned you · owner/repo", "#commit:a00000000002"],
  ["GitSocial", "Alice assigned you an issue · owner/repo", "#commit:a00000000003"],
  ["GitSocial", "Alice requested your review · owner/repo", "#commit:a00000000004"],
], "three items give one notification each, with the shared text and the item route");
const four = GS.notifyMessages(GS.pickNotifyItems(fresh, all, ME), "My Site", "owner/repo");
eq(four.map((m) => [m.title, m.body, m.route]), [["My Site", "4 new notifications", "#/timeline"]], "more than three give one summary that opens the timeline");
eq(GS.notifyMessages(GS.pickNotifyItems([assigned, review], all, ""), "GitSocial", "owner/repo").map((m) => m.body),
  ["Alice opened an issue · owner/repo", "Alice opened a pull request · owner/repo"], "without an email the body names the kind of the item");

const notes = [];
global.window.Notification = function (title, opts) { this.close = () => { this.closed = true; }; notes.push({ title, opts, n: this }); };
global.window.focus = () => { global.__focused = true; };
GS.showNotifications(three, "icon.svg");
eq(notes.map((n) => [n.title, n.opts.body, n.opts.icon, n.opts.tag]), three.map((m) => [m.title, m.body, "icon.svg", m.route.slice("#commit:".length)]), "each message is one browser notification with the icon and the item as its tag");
global.location.hash = "#/";
notes[1].n.onclick();
eq([global.location.hash, !!global.__focused, !!notes[1].n.closed], ["#commit:a00000000003", true, true], "a click focuses the tab, opens the item route and closes the notification");
global.window.Notification = function () { throw new TypeError("Illegal constructor"); };
let threw = null;
try { GS.showNotifications(three, ""); } catch (e) { threw = e; }
eq(threw, null, "a browser that refuses the constructor shows nothing without a throw");

console.log(pass + " passed, " + fail + " failed");
process.exit(fail ? 1 : 0);
