// The smallest DOM the page's builders actually touch.
//
// It is not a browser and does not try to be one. It exists so that every
// branch of the form builder and the editor can be *run* outside one, which is
// what catches the typo, the undefined call and the wrong property name that a
// syntax check cannot see. Anything it does not implement, the builders do not
// use; when that stops being true a check throws and says which method.
"use strict";

class ClassList {
  constructor(node) { this.node = node; this.set = new Set(); }
  add(...c) { for (const x of c) if (x) this.set.add(x); }
  remove(...c) { for (const x of c) this.set.delete(x); }
  toggle(c, on) { if (on === undefined) on = !this.set.has(c); on ? this.set.add(c) : this.set.delete(c); }
  contains(c) { return this.set.has(c); }
}
class Node {
  constructor(tag, ns) {
    this.tagName = String(tag).toUpperCase();
    this.ns = ns || null;
    this.children = [];
    this.attrs = {};
    this.classList = new ClassList(this);
    this.style = { setProperty() {}, removeProperty() {} };
    this.dataset = {};
    this._text = "";
    this.hidden = false;
    this.disabled = false;
    this.value = "";
    this.checked = false;
  }
  get className() { return [...this.classList.set].join(" "); }
  set className(v) { this.classList.set = new Set(String(v).split(/\s+/).filter(Boolean)); }
  get textContent() { return this._text || this.children.map(c => c.textContent).join(""); }
  set textContent(v) { this._text = String(v); this.children = []; }
  set innerHTML(v) { this._text = String(v).replace(/<[^>]*>/g, ""); this.children = []; }
  get innerHTML() { return this._text; }
  get firstChild() { return this.children[0]; }
  get lastChild() { return this.children[this.children.length - 1]; }
  append(...kids) { for (const k of kids) if (k) this.children.push(typeof k === "string" ? new Text(k) : k); }
  setAttribute(k, v) { this.attrs[k] = String(v); }
  getAttribute(k) { return this.attrs[k]; }
  removeAttribute(k) { delete this.attrs[k]; }
  querySelector() { return null; }
  querySelectorAll() { return []; }
  remove() {}
  scrollIntoView() {}
  focus() {}
  select() {}
  setSelectionRange() {}
  addEventListener() {}
  dispatchEvent() {}
  // Walk every node for assertions.
  *walk() { yield this; for (const c of this.children) if (c.walk) yield* c.walk(); }
}
class Text extends Node {
  constructor(t) { super("#text"); this._text = t; }
}
// Nodes the page addresses by id: #app, #toasts, #modal, #topbar. They are
// kept rather than minted per call, because the shell holds on to the one it
// got at load and paints into it later: a fresh node each time would mean
// nothing a check could look at afterwards.
const byId = new Map();

const doc = {
  createElement(tag) { return new Node(tag); },
  createElementNS(ns, tag) { return new Node(tag, ns); },
  createTextNode(t) { return new Text(t); },
  createDocumentFragment() { return new Node("#fragment"); },
  getElementById(id) {
    if (!byId.has(id)) byId.set(id, new Node("div"));
    return byId.get(id);
  },
  // The shell asks for the page wrapper by class to widen it for a workspace.
  // Handing back a node is enough: nothing reads anything off it.
  querySelector() { return new Node("div"); },
  querySelectorAll() { return []; },
  execCommand() { return false; },
};
module.exports = { doc, Node, Text };
