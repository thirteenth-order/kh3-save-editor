// Difficulty: the one axis the whole page turns on, so name, color and sigil
// live together and everything else indexes into this.
//
// It is its own module because two of them need it and neither owns it. The
// shell draws the segmented control out of it and the confirm dialog draws the
// before-and-after chips, so putting it in either one would have the other
// importing from a file about something else.
// Modules are always strict, so there is no "use strict" here. They are
// modules because the server hands every asset out under a path that carries
// the run's token, so a relative import inherits it; see assetPrefix in
// server.go for why the query string could not do that job.

export const DIFFS = [
  { name: "Beginner", hue: "var(--beginner)", sig: "sig0" },
  { name: "Standard", hue: "var(--standard)", sig: "sig1" },
  { name: "Proud",    hue: "var(--proud)",    sig: "sig2" },
  { name: "Critical", hue: "var(--critical)", sig: "sig3" },
];

// A save with a difficulty byte outside 0-3 is corrupt or from a future format
// version. Fall back rather than throwing, so one bad slot cannot stop the
// whole page rendering.
export const UNKNOWN_DIFF = { name: "unknown", hue: "var(--dim)", sig: "sig0" };

export function diffMeta(i) { return DIFFS[i] || UNKNOWN_DIFF; }

// Critical is the only difficulty that changes anything outside the flag.
export const CRITICAL = 3;
