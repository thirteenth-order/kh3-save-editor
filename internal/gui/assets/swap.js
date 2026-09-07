// The difficulty swap, which is the one edit this program makes that is not a
// field write: it moves HP, MP and three ability words as well as the flag.
// The panel is a disclosure in the workspace header rather than the front page
// because a tool that maps the whole format should not read as a difficulty
// switcher with an editor bolted on.

import { CRITICAL, DIFFS } from "./diffs.js";
import { api, ask, chip, el, icon, optionSwitch, pill, toast } from "./ui.js";

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


export function difficultyPanel(slot, session, onSwapped) {
  const wrap = el("div", "dpanel");
  const apply = pill("Apply", "i-check", "go");
  const hint = el("span", "hint");
  const out = el("div");

  // The Soldier's Earring is the one thing Critical starts with that the
  // difficulty flag does not carry, so it is the only choice a swap has. Both
  // switches appear only when they would change something, and start in the
  // state that matches what the game would have done: Critical hands the
  // earring out, leaving Critical takes it back.
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
      hint.append(document.createTextNode("add the missing Soldier’s Earring"));
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
        // A swap writes the file, so whatever is unsaved in the editor is
        // about to be measured against a save it no longer describes. Saying
        // so here is cheaper than discovering it after the reload.
        sub: session.dirty()
          ? "A timestamped backup is written first. Unsaved edits in the editor will be dropped: the file changes underneath them."
          : "A timestamped backup is written before anything is touched.",
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
      seg.repaint(picked);
      toast(sameDifficulty
        ? slot.slot + " got its Soldier’s Earring"
        : slot.slot + " is now " + DIFFS[picked].name, "good");
      onSwapped();
      return;
    } catch (e) {
      out.append(el("div", "hint err", e.message));
      toast(e.message, "bad");
    }
    label.textContent = "Apply";
    refresh();
  };

  const act = el("div", "act");
  act.append(apply, hint);
  wrap.append(seg.node, extras, act, out);
  return wrap;
}
