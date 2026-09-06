// The schema-driven half of the editor: it loads the field description the
// server publishes at /api/schema and turns it into two things, a form and a
// validator. Neither knows anything about Kingdom Hearts. Everything they show
// -- which fields exist, what they are called, which table names their ids,
// what range the format allows -- comes out of that one document, which is the
// same description the format tests hold against a dump. Add a field to the
// save and it appears here; there is no widget to write.
"use strict";

let SCHEMA = null;
let TABLES = null; // name -> {list:[{id,name}], byId:Map}

async function loadSchema() {
  if (SCHEMA) return SCHEMA;
  const s = await api("/api/schema");
  TABLES = {};
  for (const name of Object.keys(s.tables || {})) {
    const list = s.tables[name];
    const byId = new Map();
    for (const e of list) byId.set(e.id, e.name);
    TABLES[name] = { list: list, byId: byId };
  }
  SCHEMA = s;
  return s;
}

function table(name) { return (TABLES && TABLES[name]) || { list: [], byId: new Map() }; }

function tableName(name, id) {
  const t = table(name).byId;
  return t.has(id) ? t.get(id) : "?";
}

/* ------------------------------------------------------------ line index -- */
// Where each key of a JSON document sits, so a validation problem can point at
// a line and the gutter can mark it. This is a scanner rather than a parser:
// it only needs to know which quoted strings are keys and how deep they are,
// which is enough to rebuild the path of every line without building values.

function indexLines(text) {
  const at = new Map();
  // Containers with no key of their own -- the document itself, and any array
  // element -- push null and contribute no path segment. A dump is objects all
  // the way down, so this only ever applies to the outermost brace.
  const stack = [];
  const pathOf = function (leaf) {
    const parts = [];
    for (const s of stack) if (s !== null) parts.push(s);
    parts.push(leaf);
    return parts.join(".");
  };
  let line = 1;
  let i = 0;
  let pendingKey = null;
  let pendingLine = 0;

  while (i < text.length) {
    const c = text[i];
    if (c === "\n") { line++; i++; continue; }
    if (c === '"') {
      const start = i;
      i++;
      while (i < text.length) {
        if (text[i] === "\\") { i += 2; continue; }
        if (text[i] === '"') break;
        if (text[i] === "\n") line++;
        i++;
      }
      const raw = text.slice(start, i + 1);
      i++;
      // A string is a key only when the next non-space character is a colon.
      let j = i;
      while (j < text.length && /\s/.test(text[j])) j++;
      if (text[j] === ":") {
        try { pendingKey = JSON.parse(raw); } catch (e) { pendingKey = null; }
        pendingLine = line;
        if (pendingKey !== null) at.set(pathOf(pendingKey), pendingLine);
      }
      continue;
    }
    if (c === "{" || c === "[") {
      stack.push(pendingKey);
      pendingKey = null;
      i++;
      continue;
    }
    if (c === "}" || c === "]") { stack.pop(); i++; continue; }
    if (c === ",") { pendingKey = null; i++; continue; }
    i++;
  }
  return at;
}

/* -------------------------------------------------------------- scalars -- */

// asNumber mirrors the server's asInt: a number, a decimal or 0x string, or a
// boolean. Anything else is not a number, and says so rather than becoming 0.
function asNumber(v) {
  if (typeof v === "number") return Number.isInteger(v) ? v : null;
  if (typeof v === "boolean") return v ? 1 : 0;
  if (typeof v === "string") {
    const t = v.trim();
    if (!t) return null;
    const n = /^[+-]?0[xX][0-9a-fA-F]+$/.test(t) ? Number(t) : Number(t);
    return Number.isInteger(n) ? n : null;
  }
  return null;
}

// scalarOf pulls the value out of an entry that may be the whole dumped object
// or just the bare number, which is what Patch accepts.
function scalarOf(entry, key) {
  if (entry && typeof entry === "object") return key in entry ? asNumber(entry[key]) : null;
  return asNumber(entry);
}

/* ------------------------------------------------------------ validation -- */
// A mirror of what Patch enforces, run in the browser so a mistake is visible
// while it is being typed rather than after a round trip. The server is still
// the authority: nothing is written without its dry run agreeing.

function validate(doc, caps) {
  const problems = [];
  const add = function (path, message, severity) {
    problems.push({ path: path, message: message, severity: severity || "err" });
  };
  if (!doc || typeof doc !== "object" || Array.isArray(doc)) {
    add("", "the document must be a JSON object", "err");
    return problems;
  }
  if (doc._format !== undefined && doc._format !== SCHEMA.docFormat) {
    add("_format", "unsupported document format " + JSON.stringify(doc._format));
  }

  const known = new Set(["_format", "_note", "account"]);
  for (const s of SCHEMA.sections) known.add(s.key);
  for (const k of Object.keys(doc)) {
    if (!known.has(k)) add(k, "there is no section called " + JSON.stringify(k));
  }
  for (const s of SCHEMA.sections) {
    if (doc[s.key] === undefined) continue;
    walkSection(s, doc[s.key], s.key, caps || {}, add);
  }
  return problems;
}

function walkSection(sec, value, path, caps, add) {
  if (sec.requires === "records" && !caps.records) {
    add(path, "this save stops before the record block, so it has no " + sec.label.toLowerCase());
    return;
  }
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    add(path, "must be an object");
    return;
  }
  switch (sec.shape) {
    case "object": return walkObject(sec, value, path, caps, add);
    case "group": return walkGroup(sec, value, path, caps, add);
    case "index": return walkIndex(sec, value, path, caps, add);
    case "names": return walkNames(sec, value, path, caps, add);
  }
}

function walkGroup(sec, value, path, caps, add) {
  const by = new Map((sec.sections || []).map(function (s) { return [s.key, s]; }));
  for (const k of Object.keys(value)) {
    const sub = by.get(k);
    if (!sub) { add(path + "." + k, "there is no " + sec.key + " group called " + JSON.stringify(k)); continue; }
    walkSection(sub, value[k], path + "." + k, caps, add);
  }
}

function walkObject(sec, value, path, caps, add) {
  const fields = new Map((sec.fields || []).map(function (f) { return [f.key, f]; }));
  const subs = new Map((sec.sections || []).map(function (s) { return [s.key, s]; }));
  for (const k of Object.keys(value)) {
    if (subs.has(k)) { walkSection(subs.get(k), value[k], path + "." + k, caps, add); continue; }
    const f = fields.get(k);
    if (!f) { add(path + "." + k, "unknown key"); continue; }
    checkField(f, value[k], path + "." + k, add);
  }
}

function walkIndex(sec, value, path, caps, add) {
  for (const k of Object.keys(value)) {
    const at = path + "." + k;
    const n = /^[+-]?(0[xX][0-9a-fA-F]+|\d+)$/.test(k.trim()) ? Number(k) : NaN;
    if (!Number.isInteger(n)) { add(at, JSON.stringify(k) + " is not an index"); continue; }
    if (n < 0 || n >= sec.count) {
      add(at, "index " + n + " is outside 0-" + (sec.count - 1));
      continue;
    }
    if (sec.entryKeys && sec.entryKeys.length) {
      const entry = value[k];
      if (!entry || typeof entry !== "object") { add(at, "must be an object"); continue; }
      for (const bk of Object.keys(entry)) {
        if (sec.entryKeys.indexOf(bk) < 0) { add(at + "." + bk, "unknown key"); continue; }
        checkEntry(sec, entry[bk], at + "." + bk, caps, add);
      }
      continue;
    }
    checkEntry(sec, value[k], at, caps, add);
  }
}

function walkNames(sec, value, path, caps, add) {
  for (const k of Object.keys(value)) {
    const at = path + "." + k;
    if (sec.keys.indexOf(k) < 0) { add(at, "there is no " + sec.key.replace(/s$/, "") + " called " + JSON.stringify(k)); continue; }
    checkEntry(sec, value[k], at, caps, add);
  }
}

// checkEntry validates one entry of an index or names section: the entry's own
// fields, then any subsections that live inside it.
function checkEntry(sec, entry, at, caps, add) {
  const fields = sec.entry || [];
  const equipSlot = fields.some(function (f) { return f.kind === "equip"; });
  if (entry === null) {
    if (!equipSlot) add(at, "null is only meaningful for an equipment slot");
    return;
  }
  if (typeof entry !== "object") {
    // A bare number stands for the single value the entry carries, which is
    // what a dump's number-shaped entries are patched back as.
    const only = fields.filter(function (f) { return f.kind !== "derived"; });
    if (only.length !== 1 && !only.some(function (f) { return f.key === "uses" || f.key === "count" || f.key === "id" || f.key === "value"; })) {
      add(at, "must be an object");
      return;
    }
    if (asNumber(entry) === null) { add(at, "is not a whole number"); return; }
    checkField(only[0], entry, at, add);
    return;
  }
  const by = new Map(fields.map(function (f) { return [f.key, f]; }));
  const subs = new Map((sec.sections || []).map(function (s) { return [s.key, s]; }));
  for (const k of Object.keys(entry)) {
    if (subs.has(k)) { walkSection(subs.get(k), entry[k], at + "." + k, caps, add); continue; }
    const f = by.get(k);
    if (!f) { add(at + "." + k, "unknown key"); continue; }
    checkField(f, entry[k], at + "." + k, add);
  }
  if (equipSlot && entry.type === undefined) {
    add(at + ".type", "an equipment entry needs a type: an id means nothing until the " +
      "type byte says which table it is an id in");
  }
}

function checkField(f, v, at, add) {
  if (!f || f.kind === "derived") return;
  if (f.kind === "text") {
    if (typeof v !== "string") { add(at, "must be a string"); return; }
    if (f.maxLen && v.length > f.maxLen) {
      add(at, "is " + v.length + " characters and the field holds " + f.maxLen);
    }
    return;
  }
  const n = asNumber(v);
  if (n === null) { add(at, "is not a whole number"); return; }
  if (f.min !== undefined && n < f.min) { add(at, n + " is below the minimum " + f.min); return; }
  if (f.max !== undefined && n > f.max) { add(at, n + " is above the maximum " + f.max); return; }
  if (f.softMin !== undefined && n < f.softMin) add(at, f.softNote, "warn");
  else if (f.softMax !== undefined && n > f.softMax) add(at, f.softNote, "warn");
  if (f.kind === "enum" && f.table && !table(f.table).byId.has(n)) {
    add(at, n + " is not an id this build knows in " + f.table, "warn");
  }
}
