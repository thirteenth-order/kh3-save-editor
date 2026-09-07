// The folder list: what was found, what the user added by hand, and the
// controls for both.
//
// It takes an onChanged callback rather than reaching for the shell's render,
// because a folder change means the scan is stale and only the shell knows
// that; passing it keeps the dependency one way.

import { platformIcon } from "./icons.js";
import { api, ask, chip, copyToClipboard, el, icon, pill, stagger, toast } from "./ui.js";

export function folderPanel(canBrowse, dirs, remembered, onChanged) {
  const wrap = el("div");
  const list = el("div");
  for (const d of dirs) {
    const row = el("div", "folder");
    // Render the masked path: this row sits beside the masked account chip,
    // and on Steam the directory is named by the account id, so showing the
    // raw path here would undo the chip. Copy and the API still use d.path.
    row.append(icon(d.archive ? "i-archive" : platformIcon(d.platform)),
      el("div", "p", d.displayPath || d.path));

    const tag = d.platform + (d.accountId ? " · " + d.accountId : "");
    if (d.archive) row.append(chip("i-archive", "zip", "hue archive"));
    row.append(chip(d.cloud ? "i-cloud" : platformIcon(d.platform), tag));

    const acts = el("div", "acts");
    const copy = pill(null, "i-copy", "quiet round tiny");
    copy.title = "copy this path";
    copy.setAttribute("aria-label", "Copy path");
    copy.onclick = function () { copyToClipboard(d.path, "Path"); };
    acts.append(copy);

    if (d.platform === "added" || d.archive) {
      const drop = pill(null, "i-x", "quiet round tiny danger");
      drop.title = "forget this folder";
      drop.setAttribute("aria-label", "Forget this folder");
      drop.onclick = async function () {
        await api("/api/folder", { path: d.path, remove: true });
        toast("Folder forgotten");
        onChanged();
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
      onChanged();
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
        if (!res.canceled) { onChanged(); return; }
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

  // Folders could only be forgotten one at a time, and only the ones that were
  // added by hand: the autodetected rows carry no remove button, because they
  // come straight back on the next scan. So a list that has drifted -- a moved
  // folder, a backup zip that no longer exists, a path added while testing --
  // had no way back to a clean slate short of editing the config file. This is
  // that way back. It clears only what the store remembers; the autodetected
  // folders are found again immediately, and nothing near a save is touched.
  if (remembered > 0) {
    const reset = pill("Forget saved folders", "i-refresh", "quiet danger");
    reset.title = "Clear the folders this app has remembered";
    reset.onclick = async function () {
      const ok = await ask({
        title: "Forget saved folders?",
        sub: "This clears only the folders this app remembers. Your saves are not touched.",
        lines: [
          remembered === 1
            ? "1 remembered folder will be forgotten."
            : remembered + " remembered folders will be forgotten.",
          "",
          "Folders in the usual locations are detected again on the next scan.",
        ],
      });
      if (!ok) return;
      const res = await api("/api/folder", { clear: true });
      toast(res.cleared === 1 ? "1 folder forgotten" : res.cleared + " folders forgotten", "good");
      onChanged();
    };
    controls.append(reset);
  }

  wrap.append(controls, msg);
  return wrap;
}
