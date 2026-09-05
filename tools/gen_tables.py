#!/usr/bin/env python3
"""Regenerate the name tables in internal/kh3/tables.go.

The tables are derived from the enums in Xeeynamo/KingdomSaveEditor (GPL-3.0),
which is why this project is GPL-3.0.

Run with --check in CI to fail if a committed table no longer matches its
source; run with no flags to regenerate.

The upstream enums carry explicit `= N` anchors that re-base the counter, so a
naive positional parse drifts by several indices. That bug put Soldier's
Earring at 187 instead of 256. The scanner below honors them, and also honors
the three shapes a naive regex gets wrong:

  hex anchors      CommandType and WorldType write `= 0x1d`, not `= 29`
  stacked tags     `[Unused] [Info("")] Usage1d,`
  multi-arg tags   `[World("bt", "Scala Ad Caelum")]` is a code plus a name
  no final comma   PlayableCharacterType ends `Unused` with no separator
"""

import argparse
import os
import re
import subprocess
import sys

UPSTREAM = "https://github.com/Xeeynamo/KingdomSaveEditor.git"
PINNED = "37a7a9dde19463a0d5f0c9588c634d6129e536f6"

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
REFS = os.path.join(ROOT, "refs", "KingdomSaveEditor")
TYPES = os.path.join(REFS, "KHSave.Lib3", "Types")

# One enum member: any number of attributes, an identifier, an optional
# `= value` anchor, and a separator that may be the closing brace.
ATTR = re.compile(r'\s*\[\s*(\w+)\s*(?:\(\s*(.*?)\s*\))?\s*\]')
IDENT = re.compile(r'\s*([A-Za-z_]\w*)')
ANCHOR = re.compile(r'\s*=\s*(0[xX][0-9a-fA-F]+|\d+)')
COMMA = re.compile(r'\s*,')
STRING = re.compile(r'"((?:[^"\\]|\\.)*)"')
COMMENT = re.compile(r'//[^\n]*|/\*.*?\*/', re.S)


class Entry:
    """One decoded enum member."""

    def __init__(self, ident, category, label, extra, unused):
        self.ident = ident
        self.category = category
        self.label = label
        self.extra = extra
        self.unused = unused


def ensure_upstream():
    if os.path.isdir(TYPES):
        return
    os.makedirs(os.path.dirname(REFS), exist_ok=True)
    print(f"cloning {UPSTREAM} at {PINNED[:12]}", file=sys.stderr)
    subprocess.run(["git", "clone", "--quiet", "--filter=blob:none", UPSTREAM, REFS], check=True)
    subprocess.run(["git", "-C", REFS, "checkout", "--quiet", PINNED], check=True)


def enum_body(src, enum_name):
    """The text between the braces of `enum enum_name`."""
    m = re.search(r'\benum\s+' + re.escape(enum_name) + r'\b', src)
    if not m:
        raise SystemExit(f"enum {enum_name} not found")
    open_brace = src.index("{", m.end())
    depth, i = 0, open_brace
    while i < len(src):
        if src[i] == "{":
            depth += 1
        elif src[i] == "}":
            depth -= 1
            if depth == 0:
                return src[open_brace + 1:i]
        i += 1
    raise SystemExit(f"enum {enum_name} is not closed")


def parse_enum(filename, enum_name):
    """Return {index: Entry}, honoring `= N` anchors.

    The scan is sequential rather than a global regex search, so a pattern can
    never re-synchronize inside an attribute's string argument and invent a
    member out of the words in a display name.
    """
    with open(os.path.join(TYPES, filename), encoding="utf-8") as fh:
        src = fh.read()
    body = COMMENT.sub(" ", enum_body(src, enum_name))

    out, idx, pos = {}, 0, 0
    while True:
        attrs = []
        while True:
            m = ATTR.match(body, pos)
            if not m:
                break
            attrs.append((m.group(1), STRING.findall(m.group(2) or "")))
            pos = m.end()

        m = IDENT.match(body, pos)
        if not m:
            break
        ident = m.group(1)
        pos = m.end()

        m = ANCHOR.match(body, pos)
        if m:
            idx = int(m.group(1), 0)
            pos = m.end()

        # An attribute that carries strings names the member; the last string
        # is the display name, because [World] puts its short code first.
        named = next((a for a in attrs if a[1]), None)
        category = next((a[0] for a in attrs if a[0] != "Unused"), "Unused" if attrs else "")
        out[idx] = Entry(
            ident=ident,
            category=category,
            label=(named[1][-1] if named and named[1][-1] else ident),
            extra=(named[1][0] if named and len(named[1]) > 1 else ""),
            unused=any(a[0] == "Unused" for a in attrs),
        )
        idx += 1

        m = COMMA.match(body, pos)
        if not m:
            break
        pos = m.end()
    return out


def q_go(s):
    return '"' + s.replace("\\", "\\\\").replace('"', '\\"') + '"'


def hex3(k):
    return f"0x{k:03X}"


# Every table rendered into tables.go, in the order they appear there.
#
#   var        the Go identifier
#   file/enum  the upstream source
#   key        how the index is written (decimal, or 0x000 for ability ids)
#   field      which decoded part of the member the table maps to
#   comment    what the index space is, because KH3 has several
# DifficultyType is deliberately absent: upstream calls level 1 "Normal" and
# the game calls it "Standard", and the CLI takes that word as an argument.
# kh3.Difficulties stays hand-written in save.go for that reason.
TABLES = [
    ("Abilities", "AbilityType.cs", "AbilityType", hex3, "label",
     "// Abilities maps an ability id to its display name. The id is the index\n"
     "// into the 512-entry ability array at character + 0x160."),
    ("AbilityCategory", "AbilityType.cs", "AbilityType", hex3, "category",
     "// AbilityCategory is Action / Support / Mobility / Info."),
    ("Items", "InventoryType.cs", "InventoryType", str, "label",
     "// Items maps an inventory index (the 0x8F4 array) to its name."),
    ("ItemCategory", "InventoryType.cs", "InventoryType", str, "category",
     "// ItemCategory is Consumable / Accessory / Synthesis / ..."),
    ("Accessories", "AccessoryType.cs", "AccessoryType", str, "label",
     "// Accessories maps an AccessoryType id, the number stored in the\n"
     "// equipped-accessory slots at character + 0xD8, a different index space\n"
     "// from Items."),
    ("Weapons", "WeaponType.cs", "WeaponType", str, "label",
     "// Weapons maps a WeaponType id, stored in the weapon slots at\n"
     "// character + 0x80. Keyblades, staves and shields share the space."),
    ("WeaponCategory", "WeaponType.cs", "WeaponType", str, "category",
     "// WeaponCategory is Keyblade / Staff / Shield, which is how a slot can be\n"
     "// offered only the weapons its character can actually hold."),
    ("Armors", "ArmorType.cs", "ArmorType", str, "label",
     "// Armors maps an ArmorType id, stored in the armor slots at\n"
     "// character + 0x98."),
    ("Consumables", "ConsumableType.cs", "ConsumableType", str, "label",
     "// Consumables maps a ConsumableType id, stored in the item slots at\n"
     "// character + 0x118."),
    ("Tents", "TentType.cs", "TentType", str, "label",
     "// Tents maps a TentType id. Item slots hold these under item type 2."),
    ("Materials", "MaterialType.cs", "MaterialType", str, "label",
     "// Materials maps a synthesis material to its slot in the 100-entry\n"
     "// u16 count array at 0x165E. Its own index space, not an inventory id."),
    ("Synthesis", "SyntesisType.cs", "SynthesisType", str, "label",
     "// Synthesis maps a SynthesisType id, the item-slot index space for\n"
     "// item type 7."),
    ("KeyItems", "KeyItemType.cs", "KeyItemType", str, "label",
     "// KeyItems maps a KeyItemType id, the item-slot index space for\n"
     "// item type 9."),
    ("Snacks", "SnackType.cs", "SnackType", str, "label",
     "// Snacks maps a SnackType id, the item-slot index space for item type 6."),
    ("Foods", "FoodType.cs", "FoodType", str, "label",
     "// Foods maps a FoodType id, the item-slot index space for item type 8."),
    ("MogItems", "MogType.cs", "MogType", str, "label",
     "// MogItems maps a MogType id, the item-slot index space for item type 10."),
    ("ItemTypes", "ItemType.cs", "ItemType", str, "label",
     "// ItemTypes names the discriminator byte in an equipment slot, which\n"
     "// selects the index space its id is read against."),
    ("Commands", "CommandType.cs", "CommandType", str, "label",
     "// Commands maps a CommandType id, stored in the shortcut, magic and link\n"
     "// arrays at 0xBF20, 0xBF50 and 0xBF68."),
    ("CommandCategory", "CommandType.cs", "CommandType", str, "category",
     "// CommandCategory is Command / Magic / Link / Info, which is what makes a\n"
     "// magic slot refuse a link and the other way round."),
    ("Locations", "LocationType.cs", "LocationType", str, "label",
     "// Locations names the save point at 0x54."),
    ("Worlds", "WorldType.cs", "WorldType", str, "label",
     "// Worlds names the world logo at 0x18."),
    ("WorldCodes", "WorldType.cs", "WorldType", str, "extra",
     "// WorldCodes gives the two-letter code a world uses in map paths, so the\n"
     "// map path at 0xBBA0 can be checked against the logo at 0x18."),
    ("CharacterIcons", "CharacterIconType.cs", "CharacterIconType", str, "label",
     "// CharacterIcons names the save-file icon at 0x60 and its DLC twin at 0x68."),
    ("PlayableCharacters", "PlayableCharacterType.cs", "PlayableCharacterType", str, "label",
     "// PlayableCharacters names the 16 character structs at 0x1880. This is the\n"
     "// slot order in the save, which is not the party id space below."),
    ("PartyCharacters", "PartyCharacter.cs", "PartyCharacter", str, "label",
     "// PartyCharacters names the ids in the five-byte party array at 0x32. A\n"
     "// different index space from PlayableCharacters: Sora is not in it."),
    ("DesireChoices", "ChoiceType.cs", "DesireChoice", str, "label",
     "// DesireChoices names the opening-choice byte at 0x30."),
    ("PowerChoices", "ChoiceType.cs", "PowerChoice", str, "label",
     "// PowerChoices names the opening-choice byte at 0x31."),
    ("AiCombatStyles", "AiCombatStyleType.cs", "AiCombatStyleType", str, "label",
     "// AiCombatStyles names the party-member AI byte at character + 0x158."),
    ("AiAbilityUse", "AiAbilityType.cs", "AiAbilityType", str, "label",
     "// AiAbilityUse names the party-member AI byte at character + 0x159."),
    ("AiRecoveryUse", "AiRecoveryType.cs", "AiRecoveryType", str, "label",
     "// AiRecoveryUse names the party-member AI byte at character + 0x15A."),
    ("RecordAttractions", "RecordAttractionType.cs", "RecordAttractionType", str, "label",
     "// RecordAttractions names the five attraction use counters at 0x696."),
    ("RecordShotlocks", "RecordShotlockType.cs", "RecordShotlockType", str, "label",
     "// RecordShotlocks names the thirty shotlock use counters at 0x6D0."),
    ("StoryFlags", "StoryFlagType.cs", "StoryFlagType", str, "label",
     "// StoryFlags names the 80-entry progress array at 0xB4C4. Each entry is a\n"
     "// story-label number, not a boolean: it counts how far that world got."),
]


def render_go(parsed):
    L = ["package kh3", "",
         "// Code generated by tools/gen_tables.py. DO NOT EDIT.",
         "//",
         "// Derived from KHSave.Lib3 (Xeeynamo/KingdomSaveEditor, GPL-3.0).",
         "//",
         "// KH3 does not have one item id space, it has about ten. An equipment slot",
         "// stores an id plus a type byte, and the type selects which of these tables",
         "// the id means something in. Mixing them up is how an editor hands someone a",
         "// keyblade that reads as a snack.", ""]

    for name, filename, enum, keyfmt, field, comment in TABLES:
        entries = parsed[(filename, enum)]
        L.append(comment)
        L.append(f"var {name} = map[int]string{{")
        for k in sorted(entries):
            v = getattr(entries[k], field)
            if field == "extra" and not v:
                continue
            L.append(f"\t{keyfmt(k)}: {q_go(v)},")
        L.append("}")
        L.append("")

    items = parsed[("InventoryType.cs", "InventoryType")]
    acc = parsed[("AccessoryType.cs", "AccessoryType")]
    by_name = {}
    for aid in sorted(acc):
        by_name.setdefault(acc[aid].label, aid)
    L.append("// ItemToAccessory bridges the two index spaces by name, so we can ask")
    L.append("// whether an inventory item is currently equipped.")
    L.append("var ItemToAccessory = map[int]int{")
    for iid in sorted(items):
        e = items[iid]
        if e.category == "Accessory" and e.label in by_name:
            L.append(f"\t{iid}: {by_name[e.label]},")
    L.append("}")
    return "\n".join(L) + "\n"


def gofmt(src):
    """Format generated Go, so the CI gofmt gate and --check agree."""
    try:
        r = subprocess.run(["gofmt"], input=src, capture_output=True, text=True)
    except FileNotFoundError:
        return src  # no Go toolchain here; the CI job that needs it has one
    if r.returncode != 0:
        raise SystemExit("gofmt rejected the generated table:\n" + r.stderr)
    return r.stdout


def build():
    ensure_upstream()
    parsed = {}
    for _, filename, enum, _, _, _ in TABLES:
        key = (filename, enum)
        if key not in parsed:
            parsed[key] = parse_enum(filename, enum)
    return {
        os.path.join(ROOT, "internal", "kh3", "tables.go"): gofmt(render_go(parsed)),
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--check", action="store_true",
                    help="fail if a committed table differs from its source")
    args = ap.parse_args()

    stale = []
    for path, content in build().items():
        rel = os.path.relpath(path, ROOT)
        if args.check:
            try:
                current = open(path, encoding="utf-8").read()
            except FileNotFoundError:
                stale.append(f"{rel} (missing)")
                continue
            if current != content:
                stale.append(rel)
            else:
                print(f"ok    {rel}")
        else:
            with open(path, "w", encoding="utf-8") as fh:
                fh.write(content)
            print(f"wrote {rel}")

    if stale:
        print("\nthese generated files are out of date:", file=sys.stderr)
        for s in stale:
            print("  " + s, file=sys.stderr)
        print("\nrun: python tools/gen_tables.py", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
