// The Document view: the code editor, the scope picker that keeps a whole dump
// navigable, and the validator's report under it.
//
// It is separate from editor.js, which is the editor widget itself -- gutter,
// highlighting, parse errors -- and knows nothing about saves. This is the
// part that knows a document has sections and that a problem has a path.

import { codeEditor, parseError } from "./editor.js";
import { SCHEMA, indexLines } from "./schema.js";
import { copyToClipboard, el, icon, pill, toast } from "./ui.js";

// paintProblems is the validator's report. It mirrors what the server would
// say, so a mistake shows up while it is being typed rather than after a round
// trip -- but the server still has the last word, and nothing is written
// without its dry run agreeing.
export function paintProblems(bar, problems, jsonView) {
  bar.innerHTML = "";
  if (!problems.length) {
    bar.className = "problems ok";
    bar.append(icon("i-check"), el("span", null, "The document checks out."));
    return;
  }
  const errs = problems.filter(function (p) { return p.severity !== "warn"; });
  bar.className = "problems " + (errs.length ? "bad" : "warn");
  const head = el("div", "phead");
  head.append(icon(errs.length ? "i-alert" : "i-warn"),
    el("span", null, errs.length
      ? errs.length + (errs.length === 1 ? " problem" : " problems") +
        (problems.length > errs.length ? ", " + (problems.length - errs.length) + " to look at" : "")
      : problems.length + (problems.length === 1 ? " thing" : " things") + " to look at"));
  bar.append(head);

  const list = el("div", "plist");
  for (const p of problems.slice(0, 40)) {
    const item = el("button", "pitem " + (p.severity === "warn" ? "warn" : "err"));
    item.type = "button";
    item.append(el("code", null, p.path || "document"), el("span", null, p.message));
    if (jsonView && p.line) item.onclick = function () { jsonView.goToLine(p.line); };
    else item.disabled = true;
    list.append(item);
  }
  if (problems.length > 40) list.append(el("p", "hint", (problems.length - 40) + " more"));
  bar.append(list);
}

// jsonPane is the document view: a scope picker, the code editor, and the
// glue that keeps the text and the document in step. Scoping exists because a
// whole dump is a lot of text to hunt through when the thing being changed is
// one character's abilities, and Patch reads a partial document anyway.
export function jsonPane(state, check) {
  const wrap = el("div", "jsonpane");

  const scopes = [{ key: "", label: "Whole document" }];
  for (const sec of SCHEMA.sections) {
    if (state.doc[sec.key] === undefined) continue;
    scopes.push({ key: sec.key, label: sec.label || sec.key });
    if (sec.shape !== "names") continue;
    for (const name of sec.keys) {
      if (state.doc[sec.key][name] === undefined) continue;
      scopes.push({ key: sec.key + "." + name, label: "  " + (sec.label || sec.key) + " · " + name });
    }
  }

  let scope = "";
  const picker = document.createElement("select");
  picker.className = "scope";
  picker.setAttribute("aria-label", "Which part of the document to edit");
  for (const s of scopes) {
    const opt = document.createElement("option");
    opt.value = s.key;
    opt.textContent = s.label;
    picker.append(opt);
  }

  function subtree() {
    if (!scope) return state.doc;
    const parts = scope.split(".");
    let at = state.doc;
    for (const p of parts) at = at[p];
    return at;
  }

  function putSubtree(v) {
    if (!scope) { state.doc = v; return; }
    const parts = scope.split(".");
    let at = state.doc;
    for (let i = 0; i < parts.length - 1; i++) at = at[parts[i]];
    at[parts[parts.length - 1]] = v;
  }

  let lines = new Map();
  let parseProblem = null;

  const code = codeEditor({
    value: JSON.stringify(subtree(), null, 2),
    onChange: function () { debounce(); },
  });

  const status = el("div", "jstatus");

  let timer = null;
  function debounce() {
    if (timer) clearTimeout(timer);
    timer = setTimeout(function () { timer = null; sync(); }, 140);
  }

  // sync parses the text and, when it parses, puts it back into the document
  // so the other two tabs and the validator see it. A document that does not
  // parse leaves the last good one alone: half-typed text is not an edit.
  function sync() {
    const text = code.get();
    try {
      const value = JSON.parse(text);
      parseProblem = null;
      putSubtree(value);
      state.dirty = true;
      lines = indexLines(text);
      status.className = "jstatus ok";
      status.textContent = text.length.toLocaleString() + " characters, " +
        text.split("\n").length.toLocaleString() + " lines";
      check();
    } catch (e) {
      parseProblem = parseError(text, e);
      lines = new Map();
      status.className = "jstatus bad";
      status.textContent = parseProblem.at
        ? "line " + parseProblem.at.line + ": " + parseProblem.message
        : parseProblem.message;
      code.mark(parseProblem.at ? [{ line: parseProblem.at.line, severity: "err" }] : []);
    }
  }

  picker.onchange = function () {
    if (!commit()) { picker.value = scope; return; }
    scope = picker.value;
    code.set(JSON.stringify(subtree(), null, 2));
    sync();
  };

  const tools = el("div", "jtools");
  const fmt = pill("Format", "i-code", "quiet tiny");
  fmt.onclick = function () {
    if (!commit()) return;
    code.set(JSON.stringify(subtree(), null, 2));
    sync();
  };
  const copy = pill("Copy", "i-copy", "quiet tiny");
  copy.onclick = function () { copyToClipboard(code.get(), "Document"); };
  tools.append(picker, fmt, copy);

  // commit is what the tab bar calls before leaving: it refuses to throw away
  // text that does not parse.
  function commit() {
    if (timer) { clearTimeout(timer); timer = null; }
    sync();
    if (!parseProblem) return true;
    toast("That is not valid JSON yet: " + parseProblem.message, "bad");
    return false;
  }

  sync();
  wrap.append(tools, code.node, status);
  return {
    node: wrap,
    commit: commit,
    goToLine: function (n) { code.goToLine(n); },
    // markProblems maps a validated path back to the line it sits on, which is
    // only possible while the text parses and only for the scope on screen.
    markProblems: function (problems) {
      if (parseProblem) return;
      const marks = [];
      for (const p of problems) {
        const rel = scope && p.path.indexOf(scope + ".") === 0
          ? p.path.slice(scope.length + 1) : (scope ? null : p.path);
        if (rel === null) continue;
        const line = lines.get(rel);
        if (!line) continue;
        p.line = line;
        marks.push({ line: line, severity: p.severity });
      }
      code.mark(marks);
    },
  };
}
