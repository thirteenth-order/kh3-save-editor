// The mapping from a platform or container form to a mark in the sprite.
// These return the mark's name, not a node, so this module depends on nothing.

/* ------------------------------------------------------------ platforms -- */
// Which mark stands for a folder or a container. The value is a directory name
// off the disk, so it is matched loosely and falls back rather than leaving a
// row with a hole in it.
//
// The three brand marks are the only artwork in this program that is not
// original: they are Simple Icons' CC0 files, and they identify a platform
// rather than claim anything about it. See the sprite in index.html and the
// Legal page.
export function platformIcon(name) {
  const at = String(name || "").toLowerCase();
  if (at.indexOf("steam") > -1) return "i-steam";
  if (at.indexOf("epic") > -1) return "i-epic";
  if (at.indexOf("zip") > -1) return "i-archive";
  if (at.indexOf("added") > -1) return "i-folder";
  return "i-stack";
}

// A container form, which is a different question from the platform: a save
// with no Steam wrapper is what a console writes, and this build has never
// been round-tripped through one, so the mark is as far as the claim goes.
export function formatIcon(format) {
  return format === "steam" ? "i-steam" : "i-playstation";
}
