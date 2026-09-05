// kh3save UI. No framework and no build step: the whole page is a few hundred
// lines of DOM calls against the local API described in server.go.
"use strict";

const TOKEN = new URLSearchParams(location.search).get("t") || "";

// The token stays in the address bar deliberately. Scrubbing it with
// history.replaceState keeps it out of browser history, but then a reload has
// no token and the page dies on a bare 403 with no way back. The trade is not
// worth it: the token is 32 bytes of crypto/rand, minted per run and dead the
// moment the process exits, so a stale copy in history cannot be replayed.
// Reload working matters more than hiding a value that expires on exit.

// Difficulty is the one axis the whole page turns on, so name, color and
// sigil live together and everything else indexes into this.
// A save with a difficulty byte outside 0-3 is corrupt or from a future
// format version. Fall back rather than throwing, so one bad slot cannot stop
// the whole page rendering.
const UNKNOWN_DIFF = { name: "unknown", hue: "var(--dim)", sig: "sig0" };
function diffMeta(i) { return DIFFS[i] || UNKNOWN_DIFF; }

const DIFFS = [
  { name: "Beginner", hue: "var(--beginner)", sig: "sig0" },
  { name: "Standard", hue: "var(--standard)", sig: "sig1" },
  { name: "Proud",    hue: "var(--proud)",    sig: "sig2" },
  { name: "Critical", hue: "var(--critical)", sig: "sig3" },
];

// Critical is the only difficulty that changes anything outside the flag.
const CRITICAL = 3;

const app = document.getElementById("app");
const toasts = document.getElementById("toasts");
const modal = document.getElementById("modal");
const topbar = document.getElementById("topbar");

// An <img> cannot set the token header, so it travels in the query string.
// Same gate, same value, just the transport the tag supports.
const EMBLEM = "/emblem.svg?t=" + encodeURIComponent(TOKEN);
for (const img of document.querySelectorAll("img.mark")) img.src = EMBLEM;

/* ------------------------------------------------------------- plumbing -- */

async function api(path, body) {
  const r = await fetch(path, {
    method: body === undefined ? "GET" : "POST",
    headers: { "X-KH3-Token": TOKEN, "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = await r.json().catch(() => ({ error: r.statusText }));
  if (!r.ok) throw new Error(data.error || r.statusText);
  return data;
}

function el(tag, cls, text) {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  if (text != null) n.textContent = text;
  return n;
}

// <use> into the sprite in index.html, so an icon costs one small element.
function icon(name) {
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  const use = document.createElementNS("http://www.w3.org/2000/svg", "use");
  use.setAttribute("href", "#" + name);
  svg.append(use);
  svg.setAttribute("aria-hidden", "true");
  svg.setAttribute("focusable", "false");
  return svg;
}

function chip(name, text, cls) {
  const n = el("span", "chip" + (cls ? " " + cls : ""));
  if (name) n.append(icon(name));
  n.append(el("span", null, text));
  return n;
}

function pill(text, name, cls) {
  const b = el("button", "pill" + (cls ? " " + cls : ""));
  b.type = "button";
  if (name) b.append(icon(name));
  if (text) b.append(el("span", null, text));
  return b;
}

function toast(text, kind) {
  const n = el("div", "toast" + (kind ? " " + kind : ""));
  n.append(icon(kind === "bad" ? "i-alert" : kind === "good" ? "i-check" : "i-sparkle"),
           el("span", null, text));
  toasts.append(n);
  setTimeout(function () {
    n.classList.add("out");
    setTimeout(function () { n.remove(); }, 320);
  }, 4600);
}

function copyToClipboard(text) {
  if (!navigator.clipboard) { toast("Clipboard is not available here", "bad"); return; }
  navigator.clipboard.writeText(text).then(
    function () { toast("Path copied", "good"); },
    function () { toast("Could not copy the path", "bad"); });
}

// Stagger the entrance so a list of saves cascades rather than snapping in.
function stagger(parent) {
  parent.classList.add("stagger");
  Array.prototype.forEach.call(parent.children, function (node, i) {
    node.style.setProperty("--n", Math.min(i, 10));
  });
}

/* ---------------------------------------------------------------- modal -- */
// Replaces window.confirm(). The native dialog blocks the whole page and
// cannot show the change list with any structure; this can, and it inherits
// Esc, focus trapping and the backdrop for free from <dialog>.

const modalYes = document.getElementById("modalYes");
const modalNo = document.getElementById("modalNo");

function ask(opts) {
  return new Promise(function (resolve) {
    let settled = false;
    function finish(ok) {
      if (settled) return;
      settled = true;
      modal.close();
      resolve(ok);
    }

    document.getElementById("modalTitle").textContent = opts.title;
    document.getElementById("modalSub").textContent = opts.sub;

    const swap = document.getElementById("modalSwap");
    swap.innerHTML = "";
    if (opts.from != null) {
      const a = chip(DIFFS[opts.from].sig, DIFFS[opts.from].name, "hue");
      a.style.setProperty("--c", DIFFS[opts.from].hue);
      const arrow = icon("i-chev");
      arrow.setAttribute("class", "arrow-mark");
      const b = chip(DIFFS[opts.to].sig, DIFFS[opts.to].name, "hue");
      b.style.setProperty("--c", DIFFS[opts.to].hue);
      swap.append(a, arrow, b);
    }

    const body = document.getElementById("modalBody");
    body.innerHTML = "";
    body.append(el("pre", "log", opts.lines.length ? opts.lines.join("\n") : "no changes"));

    modalYes.onclick = function () { finish(true); };
    modalNo.onclick = function () { finish(false); };
    modal.onclose = function () { finish(false); };
    // Clicking the dimmed backdrop is the same as canceling.
    modal.onclick = function (e) { if (e.target === modal) finish(false); };

    modal.showModal();
    modalNo.focus();
  });
}

/* -------------------------------------------------------------- banners -- */

function banner(tone, name, title, text) {
  const n = el("div", "banner " + tone);
  const badge = el("div", "ico");
  badge.append(icon(name));
  const copy = el("div");
  copy.append(el("b", null, title), el("p", null, text));
  n.append(badge, copy);
  return n;
}

/* ---------------------------------------------------- segmented control -- */

function segment(currentIndex, onPick) {
  const seg = el("div", "seg");
  seg.setAttribute("role", "group");
  seg.setAttribute("aria-label", "Difficulty");
  seg.append(el("span", "seg-thumb"));

  let picked = currentIndex;
  const buttons = DIFFS.map(function (entry, i) {
    const b = el("button");
    b.type = "button";
    b.append(icon(entry.sig), el("span", null, entry.name));
    b.onclick = function () { picked = i; paint(); onPick(picked); };
    seg.append(b);
    return b;
  });

  // Arrow keys move within the group, which is what a segmented control is
  // expected to do and what makes it usable without a mouse.
  seg.onkeydown = function (e) {
    const step = e.key === "ArrowRight" ? 1 : e.key === "ArrowLeft" ? -1 : 0;
    if (!step) return;
    e.preventDefault();
    picked = (picked + step + DIFFS.length) % DIFFS.length;
    paint();
    buttons[picked].focus();
    onPick(picked);
  };

  function paint() {
    seg.style.setProperty("--at", picked);
    seg.style.setProperty("--pick", DIFFS[picked].hue);
    buttons.forEach(function (b, i) {
      b.setAttribute("aria-pressed", i === picked ? "true" : "false");
      b.setAttribute("data-now", i === currentIndex ? "true" : "false");
      b.tabIndex = i === picked ? 0 : -1;
      b.title = i === currentIndex ? DIFFS[i].name + " (current setting)" : DIFFS[i].name;
    });
  }
  paint();

  return { node: seg, repaint: function (now) { currentIndex = now; picked = now; paint(); } };
}

/* ---------------------------------------------------------------- slots -- */

function slotCard(slot, index) {
  const box = el("div", "slot");
  const head = el("div", "slot-head");
  head.append(el("div", "idx", String(index + 1).padStart(2, "0")));

  if (slot.error) {
    const bad = el("div", "cur");
    bad.style.setProperty("--c", "var(--critical)");
    bad.append(icon("i-alert"), el("span", null, slot.slot));
    head.append(bad);
    box.style.setProperty("--c", "var(--critical)");
    box.append(head);
    const facts = el("div", "facts");
    facts.append(chip("i-alert", slot.error));
    box.append(facts);
    return box;
  }

  if (slot.slot === "system") {
    const sys = el("div", "cur");
    sys.style.setProperty("--c", "var(--dim)");
    sys.append(icon("i-slider"), el("span", null, "system"));
    head.append(sys);
    box.append(head);
    const facts = el("div", "facts");
    facts.append(chip("i-stack", "shared config"), chip(null, "no difficulty"));
    box.append(facts);
    return box;
  }

  const tone = diffMeta(slot.difficulty).hue;
  box.style.setProperty("--c", tone);

  const cur = el("div", "cur");
  cur.style.setProperty("--c", tone);
  const curIcon = icon(diffMeta(slot.difficulty).sig);
  cur.append(curIcon, el("span", null, slot.difficultyName));
  head.append(cur, el("div", "grow"));
  if (slot.path.indexOf(".zip!") > -1) head.append(chip("i-archive", "in zip", "hue archive"));
  head.append(chip(null, slot.slot, "mono"));
  box.append(head);

  const facts = el("div", "facts");
  facts.append(
    chip("i-level", "level " + slot.level),
    chip("i-clock", slot.playtime),
    chip("i-coin", slot.munny + " munny"));
  // A save that has not reached a named map yet would otherwise render an
  // empty pill with nothing but the pin in it.
  if (slot.location) facts.append(chip("i-pin", slot.location, "mono"));
  box.append(facts);

  const apply = pill("Apply", "i-check", "go");
  const hint = el("span", "hint");
  const out = el("div");

  // The Soldier's Earring is the one thing Critical starts with that the
  // difficulty flag does not carry, so it is the only choice a swap has. Both
  // switches live in the save they act on, appear only when they would change
  // something, and start in the state that matches what the game would have
  // done: Critical hands the earring out, leaving Critical takes it back.
  const extras = el("div", "extras");
  const grant = optionSwitch(
    "Add the Soldier&rsquo;s Earring that Critical starts with (+6&nbsp;AP)", true);
  const revoke = optionSwitch(
    "Take back the unequipped Soldier&rsquo;s Earring", true);
  extras.append(grant.node, revoke.node);
  grant.input.onchange = refresh;
  revoke.input.onchange = refresh;

  let picked = slot.difficulty;
  let showGrant = false;
  let showRevoke = false;
  const seg = segment(slot.difficulty, function (next) { picked = next; refresh(); });

  function refresh() {
    // Granting is only ever offered where it would add an earring that is not
    // there; revoking only where there is exactly the starting one to take.
    showGrant = picked === CRITICAL && slot.canGrantStartItem;
    showRevoke = slot.difficulty === CRITICAL && picked !== CRITICAL && slot.canRevokeStartItem;
    grant.node.hidden = !showGrant;
    revoke.node.hidden = !showRevoke;
    extras.hidden = !showGrant && !showRevoke;

    const itemOnly = picked === slot.difficulty && showGrant && grant.input.checked;
    apply.disabled = picked === slot.difficulty && !itemOnly;

    hint.innerHTML = "";
    if (picked !== slot.difficulty) {
      const arrow = el("span", "arrow");
      arrow.append(document.createTextNode(slot.difficultyName), icon("i-chev"),
                   document.createTextNode(DIFFS[picked].name));
      hint.append(arrow);
    } else if (itemOnly) {
      hint.append(document.createTextNode("add the missing Soldier\u2019s Earring"));
    } else {
      hint.append(document.createTextNode("current setting"));
    }
  }
  refresh();

  apply.onclick = async function () {
    const sentGrant = showGrant && grant.input.checked;
    const sentRevoke = showRevoke && revoke.input.checked;
    const opts = {
      path: slot.path,
      difficulty: picked,
      grantStartItems: sentGrant,
      revokeStartItems: sentRevoke,
    };
    const sameDifficulty = picked === slot.difficulty;
    apply.disabled = true;
    const label = apply.lastChild;
    label.textContent = "Checking…";
    out.innerHTML = "";
    try {
      const preview = await api("/api/swap", Object.assign({}, opts, { dryRun: true }));
      const ok = await ask({
        title: (sameDifficulty ? "Update " : "Change ") + slot.slot + "?",
        sub: "A timestamped backup is written before anything is touched.",
        // Nothing to show as a transition when only the inventory changes.
        from: sameDifficulty ? null : slot.difficulty, to: picked,
        lines: preview.changes,
      });
      if (!ok) { label.textContent = "Apply"; refresh(); return; }

      label.textContent = "Writing…";
      const res = await api("/api/swap", opts);
      out.append(el("pre", "log", res.changes.join("\n")));
      if (res.backup) {
        const done = el("div", "facts");
        done.append(chip("i-shield", "backup → " + res.backup, "mono path"));
        out.append(done);
      }
      // A switch is only ever offered when it would act, so if one was sent it
      // did act, and the save now holds the opposite inventory state.
      if (sentGrant) { slot.canGrantStartItem = false; slot.canRevokeStartItem = true; }
      if (sentRevoke) { slot.canGrantStartItem = true; slot.canRevokeStartItem = false; }

      slot.difficulty = picked;
      slot.difficultyName = DIFFS[picked].name;
      curIcon.firstChild.setAttribute("href", "#" + DIFFS[picked].sig);
      cur.lastChild.textContent = slot.difficultyName;
      cur.style.setProperty("--c", DIFFS[picked].hue);
      box.style.setProperty("--c", DIFFS[picked].hue);
      seg.repaint(picked);
      box.classList.add("done");
      setTimeout(function () { box.classList.remove("done"); }, 1000);
      toast(sameDifficulty
        ? slot.slot + " got its Soldier\u2019s Earring"
        : slot.slot + " is now " + DIFFS[picked].name, "good");
    } catch (e) {
      out.append(el("div", "hint err", e.message));
      toast(e.message, "bad");
    }
    label.textContent = "Apply";
    refresh();
  };

  const act = el("div", "act");
  act.append(apply, hint);
  box.append(seg.node, extras, act, out);
  return box;
}

/* -------------------------------------------------------------- folders -- */

function folderPanel(canBrowse, dirs) {
  const wrap = el("div");
  const list = el("div");
  for (const d of dirs) {
    const row = el("div", "folder");
    // Render the masked path: this row sits beside the masked account chip,
    // and on Steam the directory is named by the account id, so showing the
    // raw path here would undo the chip. Copy and the API still use d.path.
    row.append(icon(d.archive ? "i-archive" : "i-folder"), el("div", "p", d.displayPath || d.path));

    const tag = d.platform + (d.accountId ? " · " + d.accountId : "");
    if (d.archive) row.append(chip("i-archive", "zip", "hue archive"));
    row.append(chip(d.cloud ? "i-cloud" : null, tag));

    const acts = el("div", "acts");
    const copy = pill(null, "i-copy", "quiet round tiny");
    copy.title = "copy this path";
    copy.setAttribute("aria-label", "Copy path");
    copy.onclick = function () { copyToClipboard(d.path); };
    acts.append(copy);

    if (d.platform === "added" || d.archive) {
      const drop = pill(null, "i-x", "quiet round tiny danger");
      drop.title = "forget this folder";
      drop.setAttribute("aria-label", "Forget this folder");
      drop.onclick = async function () {
        await api("/api/folder", { path: d.path, remove: true });
        toast("Folder forgotten");
        render();
      };
      acts.append(drop);
    }
    row.append(acts);
    list.append(row);
  }
  stagger(list);
  wrap.append(list);

  const controls = el("div", "controls");
  const field = el("div", "field");
  const input = document.createElement("input");
  input.type = "text";
  // One field for both kinds. Whether a path names a folder or a backup .zip
  // is worked out when it is added, so there is nothing to choose here.
  input.placeholder = "Paste a save folder or a backup .zip, then press Enter";
  input.setAttribute("aria-label", "Path to a save folder or a backup .zip");
  field.append(icon("i-search"), input);

  const msg = el("div", "hint");

  async function submit(path) {
    if (!path) return;
    msg.textContent = "";
    msg.className = "hint";
    try {
      await api("/api/folder", { path: path });
      toast("Folder added", "good");
      render();
    } catch (e) {
      msg.className = "hint err";
      msg.textContent = e.message;
    }
  }
  input.onkeydown = function (e) { if (e.key === "Enter") submit(input.value.trim()); };

  if (canBrowse) {
    const browse = pill("Browse…", "i-folder");
    browse.onclick = async function () {
      const label = browse.lastChild;
      browse.disabled = true;
      label.textContent = "Choosing…";
      try {
        const res = await api("/api/browse", {});
        if (!res.canceled) { render(); return; }
      } catch (e) {
        msg.className = "hint err";
        msg.textContent = e.message;
      }
      browse.disabled = false;
      label.textContent = "Browse…";
    };
    controls.append(browse);
  }
  controls.append(field);
  wrap.append(controls, msg);
  return wrap;
}

/* --------------------------------------------------------------- layout -- */

function heading(name, text) {
  const h = el("h2");
  h.append(icon(name), el("span", null, text));
  return h;
}

// The label wraps its own input, so these need no ids and can be built once
// per save rather than once per page.
function optionSwitch(html, on) {
  const label = el("label", "opt");
  const input = document.createElement("input");
  input.type = "checkbox";
  input.checked = !!on;
  const track = el("span", "track");
  const copy = el("span");
  copy.innerHTML = html;
  label.append(input, track, copy);
  return { node: label, input: input };
}

async function render() {
  let data;
  try {
    data = await api("/api/scan");
  } catch (e) {
    app.innerHTML = "";
    app.append(banner("hot", "i-alert", "Could not read saves", e.message));
    return;
  }

  app.innerHTML = "";

  if (data.gameRunning) {
    app.append(banner("hot", "i-play", "Kingdom Hearts III appears to be running",
      "Close it first. The game rewrites the whole save when it saves, so a change made now would be overwritten."));
  }
  if (data.dirs && data.dirs.some(function (d) { return d.archive; })) {
    app.append(banner("zip", "i-archive", "One of these is a backup archive",
      "Saves inside a .zip can be read and edited here, and the whole archive is backed up before it is rewritten. The game cannot read a zip, so unpack it back into your save folder before playing."));
  }
  if (data.dirs && data.dirs.some(function (d) { return d.cloud; })) {
    app.append(banner("warm", "i-cloud", "Steam Cloud is on for this save",
      "Turn Cloud off for the game in its Steam properties before applying, then back on once the change has loaded. Otherwise Steam can restore its own copy over the edit."));
  }

  if (!data.dirs || !data.dirs.length) {
    const blank = el("div", "empty");
    const art = el("div", "art");
    const img = document.createElement("img");
    img.src = EMBLEM;
    img.alt = "";
    const halo = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    halo.setAttribute("viewBox", "0 0 200 200");
    halo.setAttribute("aria-hidden", "true");
    const use = document.createElementNS("http://www.w3.org/2000/svg", "use");
    use.setAttribute("href", "#rose");
    halo.append(use);
    art.append(img, halo);
    blank.append(art, el("h3", null, "No saves found yet"),
      el("p", null, "Checked Documents, OneDrive and the usual Proton prefixes. Point us at the folder once and it will be remembered."));
    app.append(blank, folderPanel(data.canBrowse, []));
    return;
  }

  app.append(heading("i-folder", "Save folders"));
  app.append(folderPanel(data.canBrowse, data.dirs));

  app.append(heading("i-stack", "Saves"));
  const saves = el("div");
  let n = 0;
  for (const d of data.dirs) for (const slot of d.slots) saves.append(slotCard(slot, n++));
  stagger(saves);
  app.append(saves);

  const again = pill("Rescan", "i-refresh", "quiet");
  again.onclick = function () { rescan(again); };
  const foot = el("div", "controls");
  foot.append(again);
  app.append(foot);
}

async function rescan(button) {
  // render() replaces the in-page button, but the one in the top bar survives,
  // so its icon has to be stopped explicitly or it spins for ever.
  const mark = button.querySelector("svg");
  if (mark) mark.classList.add("spin");
  try {
    await render();
    toast("Saves rescanned");
  } finally {
    if (mark) mark.classList.remove("spin");
  }
}

document.getElementById("topRescan").onclick = function () {
  rescan(this);
  scrollTo({ top: 0 });
};

// The masthead is tall on purpose; the condensed bar takes over once it goes.
addEventListener("scroll", function () {
  topbar.classList.toggle("on", scrollY > 170);
}, { passive: true });

render();
