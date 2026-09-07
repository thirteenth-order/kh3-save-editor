#!/usr/bin/env python3
"""Regenerate the rose-window artwork in internal/gui/assets.

Writes emblem.svg and icon.svg, and rewrites the <symbol id="rose"> and
<symbol id="ring"> blocks inside index.html. Run with --check in CI to fail if
a committed asset no longer matches.

What this is copying
--------------------
The look is the one the original emblem already had, and it is deliberately
kept: concentric courses of radiating wedges alternating navy and gold, running
edge to edge so the colour is a continuous field; a fine gold net of crossing
arcs laid over an outer band; a solid gold hub ring around a dark centre; a gold
rim. Stained glass: broad panes of colour, thin lead over the top.

What was wrong with it
----------------------
Only the setting-out. The pieces were placed by eye, so nothing met anything:

  * The backdrop stacked three symmetries with no common divisor: eight
    filled wedges, sixteen spokes, twelve circles. Half the spokes fell on a
    wedge edge and half bisected a gap.
  * Its lattice circles were sized by eye (r=23.27 on a ring of 58, where
    touching needs 15.01), so they overlapped into a scribble, crossed at no
    particular radius, and punched through the hub ring.
  * They also stopped at 81 while the rim was at 90, leaving a dead annulus.
  * The favicon's comment promised twelve wedges and drew six, gold on a gold
    field, so it read as a turbine.
  * The emblem itself was a 117 KB PNG of a fourth, unrelated drawing: soft at
    74 px on a HiDPI screen, and sharing no radius with the other two.

The setting-out
---------------
Two rules do all of it, and both are forced rather than chosen.

Wedges. A course of N wedges spans one band edge to edge, boundaries on the N
mullion rays, so consecutive wedges share an edge and the ring closes with no
gap and no overlap. Both courses carry the same N on the same rays and the same
colours, so a gold ray runs unbroken from the hub ring to the net and the mid
ring reads as a lead crossing it, which is what the original does. Splitting
the colour at the mid ring instead turns the rays into a checkerboard and loses
the radiating read entirely; that was tried, and it is worse.

The net. The outer band is woven by arcs struck from the band's own mid-ring --
the one circle it already has, each landing on the outer ring `span` mullions
away, which forces its radius by the law of cosines:

    rho^2 = Rc^2 + Ro^2 - 2*Rc*Ro*cos(span * 2*pi/N)

`span` is the only free number in the whole drawing, and it means one legible
thing: how far around an arc reaches before the rim. The circles run past the
band, so they are clipped to it: what a net shows is the part of itself
inside its frame.

Everything here is original geometry: circles, tangency, star polygons and
radiating wedges are architectural, not game artwork. Nothing from Square Enix
or Disney is bundled.
"""

import argparse
import math
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ASSETS = os.path.join(ROOT, "internal", "gui", "assets")

# Sampled off the original emblem so the redraw keeps its palette, and shared
# with app.css so page and window stay one set of colours.
INK = "#03081a"       # page ground, and the dark centre
GLASS = "#152a4c"     # navy glass, shadowed side
GLASS_HI = "#1f3a63"  # navy glass, lit side
GOLD = "#a8834a"      # gold glass, shadowed side
GOLD_HI = "#cfa965"   # gold glass, lit side
LEAD = "#dfba73"      # the lead: mullions, net, rings, rim (--brass)
LEAD_HI = "#f3dcae"   # --brass-hi, the lit edge of the lead
SHADOW = "#050d1f"    # the dark seat under the lead

# Radii as fractions of the disc, measured off the original emblem with a
# radial histogram. Keeping them is what keeps the redraw recognisably the
# same mark rather than a new one.
R_CENTRE = 0.105      # dark centre
R_HUB = 0.205         # solid gold hub ring, outer edge
R_MID = 0.495         # between the two wedge courses
R_NET = 0.720         # between the outer course and the net band
R_BAND = 0.945        # net band, outer edge
R_RIM = 0.958         # gold rim, inner edge

# Twenty-four rays, and gold narrower than navy in both courses. Both come off
# the original: an angular scan of it finds ten to twelve gold runs per turn,
# and gold holding about a quarter of the circumference inside the mid ring
# against just under half outside it.
N_WEDGE = 24
DUTY_IN = 0.62        # gold ray width inside the mid ring, as a share of a slot
DUTY_OUT = 0.86       # and outside it
DUTY_ICON = 0.50      # and in the favicon, which has only one course


def f(x):
    """Trim a float to 3dp without trailing zeros, so paths stay readable."""
    return f"{x:.3f}".rstrip("0").rstrip(".") or "0"


def pt(cx, cy, r, deg):
    a = math.radians(deg)
    return cx + r * math.cos(a), cy + r * math.sin(a)


def attrs(kw):
    return " ".join(f'{k.replace("_", "-")}="{v}"' for k, v in kw.items())


class Course:
    """N wedges filling the band [inner, outer], edge to edge.

    `phase` rotates the course. The outer course carries twice the wedges of
    the inner one at the same phase, so an inner mullion continues outward as a
    mullion rather than landing in the middle of a wedge.
    """

    def __init__(self, n, cx, cy, inner, outer, phase=0.0):
        self.n, self.cx, self.cy = n, cx, cy
        self.inner, self.outer = inner, outer
        self.step = 360.0 / n
        self.edge_angles = [-90 + phase + i * self.step for i in range(n)]

    def circle(self, r, **kw):
        return (f'<circle cx="{f(self.cx)}" cy="{f(self.cy)}" r="{f(r)}" '
                f'{attrs(kw)}/>')

    def wedge(self, i, duty=1.0):
        """One wedge: the annular sector between two neighbouring mullions.

        `duty` narrows it about its own centre line. The original is navy
        dominant (gold runs about a quarter of the circumference inside the
        mid ring and just under half outside it) and equal wedges lose that,
        reading as a gold sunburst instead of blue glass with gold in it.
        """
        pad = self.step * (1 - duty) / 2
        a0 = self.edge_angles[i] + pad
        a1 = self.edge_angles[i] + self.step - pad
        big = 1 if (a1 - a0) > 180 else 0
        x0, y0 = pt(self.cx, self.cy, self.outer, a0)
        x1, y1 = pt(self.cx, self.cy, self.outer, a1)
        x2, y2 = pt(self.cx, self.cy, self.inner, a1)
        x3, y3 = pt(self.cx, self.cy, self.inner, a0)
        return (f"M{f(x0)} {f(y0)}"
                f"A{f(self.outer)} {f(self.outer)} 0 {big} 1 {f(x1)} {f(y1)}"
                f"L{f(x2)} {f(y2)}"
                f"A{f(self.inner)} {f(self.inner)} 0 {big} 0 {f(x3)} {f(y3)}Z")

    def wedges(self, parity, duty=1.0):
        return "".join(self.wedge(i, duty) for i in range(self.n)
                       if i % 2 == parity)

    def mullions(self, r0=None, r1=None):
        r0 = self.inner if r0 is None else r0
        r1 = self.outer if r1 is None else r1
        d = []
        for a in self.edge_angles:
            x0, y0 = pt(self.cx, self.cy, r0, a)
            x1, y1 = pt(self.cx, self.cy, r1, a)
            d.append(f"M{f(x0)} {f(y0)}L{f(x1)} {f(y1)}")
        return "".join(d)

    def star(self, r_out, r_in):
        """A 2N-point star polygon, points on the mullion rays."""
        d = []
        for i in range(2 * self.n):
            r = r_out if i % 2 == 0 else r_in
            x, y = pt(self.cx, self.cy, r,
                      self.edge_angles[0] + i * self.step / 2)
            d.append(("M" if i == 0 else "L") + f"{f(x)} {f(y)}")
        return "".join(d) + "Z"


class Net:
    """N arcs sweeping across the band [inner, outer], crossing each other.

    The original's outer band is not a chain of beads and not a row of spikes;
    it is shallow arcs that cross to weave the band into curved quadrilaterals.
    Two rules and one integer produce that:

      * the arcs are struck from the band's own mid-ring, the one circle the
        band already has without choosing anything;
      * each arc lands on the outer ring `span` mullions away, which by the law
        of cosines forces its radius:

            rho^2 = Rc^2 + Ro^2 - 2*Rc*Ro*cos(span * 2*pi/N)

    `span` is the only free number in the drawing, and it means one legible
    thing: how far around an arc reaches before the rim. Rendered and compared
    against the original at 2 through 5: at 2 the arcs cross almost at once and
    the band is a row of narrow lenses; by 4 they are shallow enough that the
    cells go triangular. 3 is where the crossings land mid-band and the cells
    come out as pointed arches, which is what the original has. Symmetry about
    the ray hands back the mirror landing for free, so one circle per mullion
    draws the whole net.
    """

    def __init__(self, n, cx, cy, inner, outer, span=3, phase=0.0):
        self.n, self.cx, self.cy = n, cx, cy
        self.inner, self.outer = inner, outer
        self.step = 360.0 / n
        self.centre = (inner + outer) / 2
        self.r = math.sqrt(max(
            self.centre ** 2 + outer ** 2
            - 2 * self.centre * outer
            * math.cos(math.radians(span * self.step)), 0.0))
        self.angles = [-90 + phase + i * self.step for i in range(n)]

    def circles(self, **kw):
        out = []
        for a in self.angles:
            x, y = pt(self.cx, self.cy, self.centre, a)
            out.append(f'<circle cx="{f(x)}" cy="{f(y)}" r="{f(self.r)}" '
                       f'{attrs(kw)}/>')
        return "".join(out)


def band_clip(cid, cx, cy, inner, outer):
    """An annulus as a clip path, so the net stops at its own band."""
    return (f'<clipPath id="{cid}"><path clip-rule="evenodd" d="'
            f'M{f(cx - outer)} {f(cy)}'
            f'a{f(outer)} {f(outer)} 0 1 0 {f(2 * outer)} 0'
            f'a{f(outer)} {f(outer)} 0 1 0 {f(-2 * outer)} 0Z'
            f'M{f(cx - inner)} {f(cy)}'
            f'a{f(inner)} {f(inner)} 0 1 0 {f(2 * inner)} 0'
            f'a{f(inner)} {f(inner)} 0 1 0 {f(-2 * inner)} 0Z'
            f'"/></clipPath>')


def radii(R, *names):
    frac = {"centre": R_CENTRE, "hub": R_HUB, "mid": R_MID,
            "net": R_NET, "band": R_BAND, "rim": R_RIM}
    return {k: frac[k] * R for k in names}


# ----------------------------------------------------------------- emblem --
def emblem():
    """The masthead mark, redrawn on the original PNG's own proportions.

    As SVG it is a few KB instead of 117, stays sharp at 74 px on a HiDPI
    screen, and is set out on the same radii as the backdrop and the favicon
    rather than merely resembling them.
    """
    S = 256.0
    c = S / 2
    R = 124.0
    lead = 1.3
    r = radii(R, "centre", "hub", "mid", "net", "band", "rim")
    inner = Course(N_WEDGE, c, c, r["hub"], r["mid"])
    outer = Course(N_WEDGE, c, c, r["mid"], r["net"])
    net = Net(N_WEDGE, c, c, r["net"], r["band"])

    o = [f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {f(S)} {f(S)}" '
         f'width="{f(S)}" height="{f(S)}" role="img" aria-label="kh3save">',
         "<defs>"]
    # One light direction for the whole window: both ramps run the same way, so
    # the two glasses read as one material at two hues rather than as swatches.
    o.append(f'<linearGradient id="e-navy" x1="0" y1="0" x2=".7" y2="1">'
             f'<stop offset="0" stop-color="{GLASS_HI}"/>'
             f'<stop offset="1" stop-color="{GLASS}"/></linearGradient>')
    o.append(f'<linearGradient id="e-gold" x1="0" y1="0" x2=".7" y2="1">'
             f'<stop offset="0" stop-color="{GOLD_HI}"/>'
             f'<stop offset="1" stop-color="{GOLD}"/></linearGradient>')
    o.append(f'<linearGradient id="e-lead" x1="0" y1="0" x2=".7" y2="1">'
             f'<stop offset="0" stop-color="{LEAD_HI}"/>'
             f'<stop offset="1" stop-color="{LEAD}"/></linearGradient>')
    o.append(band_clip("e-band", c, c, r["net"], r["band"]))
    o.append("</defs>")

    # Ground, then glass, then lead over the top of it: broad panes of colour
    # with thin lead across them, which is the whole of the stained-glass read.
    # Navy is the ground rather than a set of wedges: with the gold narrowed,
    # what is left between the gold rays is a single continuous field, which is
    # also one less path to keep in step.
    o.append(f'<circle cx="{f(c)}" cy="{f(c)}" r="{f(R)}" '
             f'fill="url(#e-navy)"/>')
    # Same parity in both courses: a ray keeps its colour the whole way out and
    # the mid ring crosses it, rather than chopping it into a checker.
    o.append(f'<path fill="url(#e-gold)" '
             f'd="{inner.wedges(1, DUTY_IN) + outer.wedges(1, DUTY_OUT)}"/>')

    # The lead is drawn twice: a dark pass a little wider, then the gold on top
    # of it. That leaves a thin dark edge on both sides of every line, which is
    # what reads as the lead standing proud of the glass. It is two strokes
    # rather than a filter because a filter with one unrecognised primitive can
    # be dropped, or in Inkscape's case turn the whole group invisible.
    o.append(f'<g fill="none" stroke="{SHADOW}" stroke-width="{f(lead + 1.1)}" '
             f'opacity=".55">')
    o.append(f'<g clip-path="url(#e-band)">{net.circles()}</g>')
    o.append(f'<path d="{inner.wedges(1, DUTY_IN)}"/>')
    o.append(f'<path d="{outer.wedges(1, DUTY_OUT)}"/>')
    o.append(f'<path d="{outer.mullions(r["net"], r["band"])}"/>')
    o.append("".join(inner.circle(r[k]) for k in ("hub", "mid", "net", "band")))
    o.append("</g>")

    o.append(f'<g fill="none" stroke="url(#e-lead)" stroke-width="{f(lead)}">')
    o.append(f'<g clip-path="url(#e-band)">{net.circles()}</g>')
    # The lead follows the panes. Stroking each gold wedge's own outline puts a
    # line down both its sides and across both its ends, which is what leaded
    # glass looks like, and it is why the mullions cannot live on the slot
    # boundaries once the gold is narrowed off them: they would run through open
    # navy, beside the pane rather than around it.
    o.append(f'<path d="{inner.wedges(1, DUTY_IN)}"/>')
    o.append(f'<path d="{outer.wedges(1, DUTY_OUT)}"/>')
    o.append(f'<path d="{outer.mullions(r["net"], r["band"])}"/>')
    o.append("".join(inner.circle(r[k]) for k in ("hub", "mid", "net", "band")))
    o.append("</g>")

    # Hub: a solid gold ring around a dark centre, stroked on its mid-radius so
    # the band edges land on R_CENTRE and R_HUB exactly.
    hw = r["hub"] - r["centre"]
    o.append(f'<circle cx="{f(c)}" cy="{f(c)}" r="{f(r["centre"] + hw / 2)}" '
             f'fill="none" stroke="url(#e-gold)" stroke-width="{f(hw)}"/>')
    o.append(f'<circle cx="{f(c)}" cy="{f(c)}" r="{f(r["centre"])}" '
             f'fill="{INK}"/>')
    o.append(f'<g fill="none" stroke="url(#e-lead)" stroke-width="{f(lead)}">'
             f'{inner.circle(r["centre"])}{inner.circle(r["hub"])}</g>')

    # Rim: a gold band from R_RIM to the disc edge, seated on a dark line so it
    # does not float off the glass, and lit on its inner edge only: lighting
    # both made it read as a bright hoop rather than as part of the window.
    rw = R - r["rim"]
    o.append(f'<circle cx="{f(c)}" cy="{f(c)}" r="{f(r["rim"])}" fill="none" '
             f'stroke="{SHADOW}" stroke-width="1.4" opacity=".5"/>')
    o.append(f'<circle cx="{f(c)}" cy="{f(c)}" r="{f(r["rim"] + rw / 2)}" '
             f'fill="none" stroke="url(#e-gold)" stroke-width="{f(rw)}"/>')
    o.append(f'<circle cx="{f(c)}" cy="{f(c)}" r="{f(r["rim"] + .5)}" '
             f'fill="none" stroke="{LEAD_HI}" stroke-width=".6" opacity=".6"/>')
    o.append("</svg>")
    return "\n".join(o) + "\n"


# ------------------------------------------------------------------- icon --
def icon():
    """The browser-tab icon: the same drawing, cut down for 16 px.

    Twenty-four rays, two courses and a net turn to mud at tab size, so what is
    dropped is detail, not the drawing: twelve rays, the emblem's own count
    halved, running the whole way from the hub ring to the rim, with the mid
    ring and the net band left out.

    Twelve and not eight. Eight was rendered and looked at: four gold blades
    around a dark hub is a radiation trefoil, and it is the same trap the old
    icon fell into from the other direction, with six gold blades on a gold
    field reading as a turbine. Twelve narrow rays stay a window.

    The mid ring was tried here too and dropped: at 32 px it closes the gap
    between the hub and the rays into one smudge.
    """
    S = 64.0
    c = S / 2
    R = 29.0
    r = radii(R, "centre", "hub", "rim")
    # One course spanning what all three of the emblem's bands span.
    course = Course(N_WEDGE // 2, c, c, r["hub"], r["rim"])
    o = [f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {f(S)} {f(S)}" '
         f'role="img" aria-label="kh3save">',
         f'<rect width="{f(S)}" height="{f(S)}" rx="14" fill="{INK}"/>',
         f'<circle cx="{f(c)}" cy="{f(c)}" r="{f(R)}" fill="{GLASS}"/>',
         f'<path fill="{LEAD}" d="{course.wedges(1, DUTY_ICON)}"/>']
    # Hub ring and rim, both stroked on their mid-radius so the band edges land
    # where the radii say. The rim is thickened: at 16 px a hairline rim drops
    # below a pixel and the mark loses its outline, which is the one part of it
    # that still works at that size.
    hw = r["hub"] - r["centre"]
    o.append(f'<circle cx="{f(c)}" cy="{f(c)}" r="{f(r["centre"] + hw / 2)}" '
             f'fill="none" stroke="{LEAD}" stroke-width="{f(hw)}"/>')
    o.append(f'<circle cx="{f(c)}" cy="{f(c)}" r="{f(r["centre"])}" '
             f'fill="{INK}"/>')
    rw = (R - r["rim"]) * 1.6
    o.append(f'<circle cx="{f(c)}" cy="{f(c)}" r="{f(R - rw / 2)}" '
             f'fill="none" stroke="{LEAD}" stroke-width="{f(rw)}"/>')
    o.append("</svg>")
    return "\n".join(o) + "\n"


# ---------------------------------------------------------- page symbols --
def rose_symbol():
    """The backdrop, at 11% opacity behind the masthead.

    The emblem's setting-out in line, with one faint wedge alternation under it
    (the same drawing at two sizes rather than two drawings that nearly agree).
    It is what the old backdrop was trying to be: it had the wedges, the spokes
    and the circle lattice, just none of them on speaking terms.
    """
    S = 200.0
    c = S / 2
    R = 96.0
    r = radii(R, "centre", "hub", "mid", "net", "band", "rim")
    inner = Course(N_WEDGE, c, c, r["hub"], r["mid"])
    outer = Course(N_WEDGE, c, c, r["mid"], r["net"])
    net = Net(N_WEDGE, c, c, r["net"], r["band"])
    o = ["    <!-- Rose window: two courses of wedges under a net of crossing",
         "         circles. Generated by tools/gen_emblem.py, which carries the",
         "         setting-out; edit it there, not here. The same drawing is the",
         "         page backdrop and the empty state. -->",
         '    <symbol id="rose" viewBox="0 0 200 200">',
         f'      <defs>{band_clip("r-band", c, c, r["net"], r["band"])}</defs>',
         # The alternation is the only thing separating a lattice from a hatch
         # once the whole symbol is down at 11%.
         f'      <path fill="currentColor" opacity=".13" '
         f'd="{inner.wedges(1, DUTY_IN) + outer.wedges(1, DUTY_OUT)}"/>',
         '      <g fill="none" stroke="currentColor" stroke-width=".9" '
         'opacity=".85">',
         f'        <g clip-path="url(#r-band)">{net.circles()}</g>',
         f'        <path d="{inner.wedges(1, DUTY_IN)}"/>',
         f'        <path d="{outer.wedges(1, DUTY_OUT)}"/>',
         f'        <path d="{outer.mullions(r["net"], r["band"])}"/>',
         "      </g>",
         '      <g fill="none" stroke="currentColor" stroke-width="1.1">',
         "        " + "".join(inner.circle(r[k]) for k in
                              ("hub", "mid", "net", "band", "rim"))
         + inner.circle(R),
         "      </g>",
         f'      <circle cx="{f(c)}" cy="{f(c)}" r="{f(r["centre"])}" '
         f'fill="currentColor" opacity=".35"/>',
         "    </symbol>"]
    return "\n".join(o)


def ring_symbol():
    """The bezel that turns around the masthead mark.

    Sixteen ticks on the emblem's own outer-course mullion rays, so as it turns
    its marks pass over the mullions beneath rather than drifting against them.
    The old ring was a 16-point star at an unrelated phase inside an unrelated
    circle, which is why it read as jitter; a 32-vertex star also resolves at
    this size into a second dense texture just outside an already dense one.
    """
    S = 24.0
    c = S / 2
    band = 11.4
    long_ticks = Course(16, c, c, band - 1.5, band)
    short_ticks = Course(16, c, c, band - .7, band, 11.25)
    o = ["    <!-- The bezel that turns around the emblem. Generated by",
         "         tools/gen_emblem.py; edit it there, not here. -->",
         '    <symbol id="ring" viewBox="0 0 24 24">',
         "      " + long_ticks.circle(band, fill="none", stroke="currentColor",
                                      stroke_width=".3", opacity=".7"),
         '      <g fill="none" stroke="currentColor" stroke-width=".5" '
         'stroke-linecap="round">',
         f'        <path d="{long_ticks.mullions()}"/>',
         f'        <path opacity=".5" d="{short_ticks.mullions()}"/>',
         "      </g>",
         "    </symbol>"]
    return "\n".join(o)


# ------------------------------------------------------------------ write --
# Each symbol owns the comment above it, so a rewritten drawing can never be
# left sitting under a description of the drawing it replaced, which is how
# index.html came to promise twelve petal circles over a lattice of eight.
ROSE_RE = re.compile(r'    <!--[^>]*?-->\n(?=    <symbol id="rose")'
                     r'|    <symbol id="rose".*?</symbol>', re.S)
RING_RE = re.compile(r'    <!--[^>]*?-->\n(?=    <symbol id="ring")'
                     r'|    <symbol id="ring".*?</symbol>', re.S)


def render_index(src):
    for pat, build in ((RING_RE, ring_symbol), (ROSE_RE, rose_symbol)):
        # Two matches: the stale comment, dropped, then the symbol it labelled.
        n = len(pat.findall(src))
        if n not in (1, 2):
            raise SystemExit(f"index.html: expected 1 or 2 matches, found {n}")
        seen = []
        src = pat.sub(lambda m: seen.append(1) or ("" if len(seen) < n
                                                   else build()), src)
    return src


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--check", action="store_true",
                    help="fail if a committed asset is stale")
    ap.add_argument("-o", "--out", default=ASSETS)
    args = ap.parse_args()

    index_path = os.path.join(args.out, "index.html")
    with open(index_path, encoding="utf-8") as fh:
        index_new = render_index(fh.read())

    want = {
        "emblem.svg": emblem(),
        "icon.svg": icon(),
        "index.html": index_new,
    }

    stale = []
    for name, body in want.items():
        path = os.path.join(args.out, name)
        try:
            with open(path, encoding="utf-8") as fh:
                same = fh.read() == body
        except FileNotFoundError:
            same = False
        if same:
            continue
        stale.append(name)
        if not args.check:
            with open(path, "w", encoding="utf-8") as fh:
                fh.write(body)

    if args.check:
        if stale:
            print("stale, run `make emblem`: " + ", ".join(sorted(stale)),
                  file=sys.stderr)
            return 1
        print("emblem assets up to date")
        return 0
    print("wrote " + ", ".join(sorted(stale)) if stale else "already up to date")
    return 0


if __name__ == "__main__":
    sys.exit(main())
