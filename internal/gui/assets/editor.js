// A JSON code editor with line numbers, highlighting and an error gutter,
// built out of a <textarea> laid over a highlighted <pre>.
//
// It is a textarea and not a contenteditable on purpose. A textarea keeps the
// caret, selection, undo stack, spellcheck suppression, IME and mobile
// keyboards that a contenteditable has to reimplement badly; the only thing it
// cannot do is color its own text, and a <pre> underneath it does that. The
// two are kept in lockstep by sharing one scroll container and one set of font
// metrics, which is why app.css declares them together rather than separately.
// Modules are always strict, so there is no "use strict" here. They are
// modules because the server hands every asset out under a path that carries
// the run's token, so a relative import inherits it; see assetPrefix in
// server.go for why the query string could not do that job.
import { el } from "./ui.js";

// Above this many characters the highlight is dropped and the editor stays
// plain. Re-tokenising on a debounce is cheap for a normal dump (a real save
// is about 30 KB) but a late-game save with every ability and a full
// inventory can be several hundred, and a keystroke should never wait on it.
const HIGHLIGHT_LIMIT = 400000;

function escapeHTML(s) {
  return s.replace(/[&<>]/g, function (c) {
    return c === "&" ? "&amp;" : c === "<" ? "&lt;" : "&gt;";
  });
}

// One pass over the text: strings (and whether a colon follows, which makes
// them keys), then the three literals, then numbers. Everything between
// matches is punctuation and whitespace and goes through untouched.
const JSON_TOKENS = /("(?:\\.|[^"\\])*")(\s*:)?|\b(true|false|null)\b|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g;

export function highlightJSON(text) {
  let out = "";
  let last = 0;
  let m;
  JSON_TOKENS.lastIndex = 0;
  while ((m = JSON_TOKENS.exec(text)) !== null) {
    out += escapeHTML(text.slice(last, m.index));
    if (m[1] !== undefined) {
      const cls = m[2] ? "t-key" : "t-str";
      out += '<span class="' + cls + '">' + escapeHTML(m[1]) + "</span>";
      if (m[2]) out += escapeHTML(m[2]);
    } else if (m[3] !== undefined) {
      out += '<span class="t-lit">' + escapeHTML(m[3]) + "</span>";
    } else {
      out += '<span class="t-num">' + escapeHTML(m[4]) + "</span>";
    }
    last = m.index + m[0].length;
  }
  return out + escapeHTML(text.slice(last));
}

// lineOf turns a character offset into a 1-based line and column, which is how
// JSON.parse reports a syntax error on every engine that reports one at all.
function lineOf(text, pos) {
  const upto = text.slice(0, pos);
  const line = upto.split("\n").length;
  return { line: line, col: pos - upto.lastIndexOf("\n") };
}

// parseError normalizes what the browsers say. Chrome gives "at position 42",
// Firefox "at line 3 column 5", Safari neither, so take whichever is there and
// fall back to the message alone.
export function parseError(text, err) {
  const msg = String(err.message || err);
  let at = null;
  let m = /at position (\d+)/.exec(msg);
  if (m) at = lineOf(text, Number(m[1]));
  if (!at) {
    m = /line (\d+) column (\d+)/.exec(msg);
    if (m) at = { line: Number(m[1]), col: Number(m[2]) };
  }
  return { message: msg.replace(/^JSON\.parse: /, ""), at: at };
}

// insert writes into the textarea through execCommand where it exists, which
// is the only way to leave the browser's own undo stack intact. Rewriting
// .value directly makes Ctrl-Z throw away everything typed since the field was
// focused, which in an editor is worse than the feature being missing.
function insert(area, text) {
  area.focus();
  if (document.execCommand && document.execCommand("insertText", false, text)) return;
  const s = area.selectionStart;
  area.setRangeText(text, s, area.selectionEnd, "end");
  area.dispatchEvent(new Event("input", { bubbles: true }));
}

export function codeEditor(opts) {
  // opts: { value, onChange(text) }
  const wrap = el("div", "code");
  const gutter = el("div", "gutter");
  const body = el("div", "code-body");
  const pre = el("pre", "hl");
  pre.setAttribute("aria-hidden", "true");
  const area = document.createElement("textarea");
  area.className = "code-in";
  area.spellcheck = false;
  area.setAttribute("wrap", "off");
  area.setAttribute("aria-label", "Save document, JSON");
  area.value = opts.value || "";

  body.append(pre, area);
  wrap.append(gutter, body);

  let marks = new Map(); // line -> "err" | "warn"
  let plain = false;

  function paintGutter() {
    const lines = area.value.split("\n").length;
    // Rebuilding the whole column on every keystroke is fine at this size and
    // is the only version that cannot drift out of step with the text.
    const frag = document.createDocumentFragment();
    for (let i = 1; i <= lines; i++) {
      const n = el("div", "gl", String(i));
      const mark = marks.get(i);
      if (mark) n.classList.add(mark);
      frag.append(n);
    }
    gutter.innerHTML = "";
    gutter.append(frag);
  }

  function paint() {
    const text = area.value;
    plain = text.length > HIGHLIGHT_LIMIT;
    wrap.classList.toggle("plain", plain);
    if (plain) pre.textContent = text + "\n";
    // The trailing newline keeps the last line's box height when the text ends
    // without one, which is what stops the caret sitting below the highlight.
    else pre.innerHTML = highlightJSON(text) + "\n";
    paintGutter();
  }

  area.addEventListener("input", function () {
    paint();
    if (opts.onChange) opts.onChange(area.value);
  });

  area.addEventListener("keydown", function (e) {
    if (e.key === "Tab") {
      e.preventDefault();
      insert(area, "  ");
      return;
    }
    if (e.key === "Enter") {
      // Carry the current line's indent, and open a level after a bracket, so
      // typing a new entry by hand does not start at column one.
      const upto = area.value.slice(0, area.selectionStart);
      const line = upto.slice(upto.lastIndexOf("\n") + 1);
      const indent = (/^[ \t]*/.exec(line) || [""])[0];
      const deeper = /[[{]\s*$/.test(line) ? "  " : "";
      e.preventDefault();
      insert(area, "\n" + indent + deeper);
      return;
    }
    if (e.key === "Escape") area.blur();
  });

  paint();

  return {
    node: wrap,
    area: area,
    get: function () { return area.value; },
    set: function (v) {
      if (area.value === v) return;
      area.value = v;
      paint();
    },
    // mark takes [{line, severity}] and repaints only the gutter.
    mark: function (list) {
      marks = new Map();
      for (const m of list || []) {
        if (!m.line) continue;
        // An error outranks a warning on the same line.
        if (marks.get(m.line) === "err") continue;
        marks.set(m.line, m.severity === "warn" ? "warn" : "err");
      }
      paintGutter();
    },
    goToLine: function (line) {
      const lines = area.value.split("\n");
      let pos = 0;
      for (let i = 0; i < Math.min(line - 1, lines.length); i++) pos += lines[i].length + 1;
      area.focus();
      area.setSelectionRange(pos, pos + (lines[line - 1] || "").length);
      const row = gutter.children[line - 1];
      if (row) row.scrollIntoView({ block: "center" });
    },
  };
}
