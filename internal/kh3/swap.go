package kh3

import "fmt"

// SwapOptions controls the parts of a difficulty change that are not the flag.
type SwapOptions struct {
	ScaleHP          bool // default on: current HP/MP follow the halving
	ScaleBonuses     bool // unverified, see README
	GrantStartItems  bool // entering Critical: add the Soldier's Earring
	RevokeStartItems bool // leaving Critical: take back a single unequipped one
}

func DefaultSwapOptions() SwapOptions { return SwapOptions{ScaleHP: true} }

const (
	bonusHPOff = 0xB49C
	bonusMPOff = 0xB4A0
)

// SwapDifficulty faithfully moves a save between difficulties.
//
// Only three things are stored state: the flag itself, the three Critical
// abilities, and current HP/MP. Everything else Critical changes (damage
// taken, EXP rate, MP charge time, situation command build-up, AP) is a
// runtime multiplier the game derives from the flag.
func SwapDifficulty(plain []byte, target byte, opt SwapOptions) ([]byte, []string, error) {
	if _, ok := Difficulties[target]; !ok {
		return nil, nil, fmt.Errorf("difficulty must be 0-3, got %d", target)
	}
	out := make([]byte, len(plain))
	copy(out, plain)
	var changes []string

	old := out[DifficultyOffset]
	if old == target {
		// Not a difficulty change, but the Critical start items can still be
		// wrong: a Critical save that never got its Soldier's Earring, or was
		// brought to Critical by an older version of this tool, is missing an
		// item the game would have given it. Everything else below is keyed to
		// the difficulty actually changing, so only the items are considered.
		return out, swapStartItems(out, old, target, opt), nil
	}
	out[DifficultyOffset] = target
	changes = append(changes, fmt.Sprintf("difficulty 0x14: %s -> %s",
		Difficulties[old], Difficulties[target]))

	changes = append(changes, swapAbilities(out, target)...)

	// Every character, not just Sora. A real Critical save proves the game
	// halves the whole party: Donald went 150/100 -> 75/50 and Goofy 100 -> 50
	// MP with this tool never touching them.
	if opt.ScaleHP && (old == Critical) != (target == Critical) {
		for ci := range CharNames {
			for _, st := range []struct {
				label string
				off   int
			}{{"HP", StatHP}, {"MP", StatMP}} {
				cur := GetStat(out, ci, st.off)
				if cur <= 0 {
					continue
				}
				nv := cur * 2
				if target == Critical {
					if nv = cur / 2; nv < 1 {
						nv = 1
					}
				}
				SetStat(out, ci, st.off, nv)
				if ci < 3 {
					changes = append(changes, fmt.Sprintf("  %s current %s: %d -> %d",
						CharNames[ci], st.label, cur, nv))
				}
			}
		}
	}

	changes = append(changes, swapStartItems(out, old, target, opt)...)

	if opt.ScaleBonuses && (old == Critical) != (target == Critical) {
		for _, b := range []struct {
			name string
			off  int
		}{{"bonus_hp", bonusHPOff}, {"bonus_mp", bonusMPOff}} {
			cur := int32(le32(out[b.off:]))
			if cur == 0 {
				continue
			}
			nv := cur * 2
			if target == Critical {
				nv = cur / 2
			}
			putLe32(out[b.off:], uint32(nv))
			changes = append(changes, fmt.Sprintf("  %s: %d -> %d", b.name, cur, nv))
		}
	}
	return out, changes, nil
}

func swapAbilities(out []byte, target byte) []string {
	var changes []string
	for _, aid := range CriticalAbilities {
		cur := GetAbility(out, Sora, aid)
		name := Abilities[aid]

		if target == Critical {
			switch {
			case cur == AbilityAbsent:
				SetAbility(out, Sora, aid, AbilityInnateEquipped)
				changes = append(changes, fmt.Sprintf(
					"  Sora ability 0x%03X %s: granted (0x%08X -> 0x%08X)",
					aid, name, cur, uint32(AbilityInnateEquipped)))
			case cur&1 != 0:
				changes = append(changes, fmt.Sprintf(
					"  %s: already owned (0x%08X), left alone", name, cur))
			default:
				changes = append(changes, fmt.Sprintf(
					"  %s: unexpected value 0x%08X, left alone", name, cur))
			}
			continue
		}

		// Leaving Critical. Remove only what Sora owns innately: if equipment
		// grants it, taking it away would desync the save from the gear that
		// is still equipped.
		if cur == AbilityAbsent {
			continue
		}
		if IsInnatelyOwned(cur) {
			SetAbility(out, Sora, aid, AbilityAbsent)
			changes = append(changes, fmt.Sprintf(
				"  Sora ability 0x%03X %s: removed (0x%08X -> 0x%08X)",
				aid, name, cur, uint32(AbilityAbsent)))
		} else {
			changes = append(changes, fmt.Sprintf(
				"  %s: granted by equipment (source 0x%X), left alone", name, cur>>12))
		}
	}
	return changes
}

func swapStartItems(out []byte, old, target byte, opt SwapOptions) []string {
	var changes []string
	for id, qty := range CriticalStartItems {
		name := Items[id]
		count, _ := GetItem(out, id)

		if opt.GrantStartItems && target == Critical {
			if count > 0 {
				changes = append(changes, fmt.Sprintf(
					"  %s: already held x%d, left alone", name, count))
				continue
			}
			SetItem(out, id, qty, ItemFresh)
			changes = append(changes, fmt.Sprintf(
				"  %s: added x%d to inventory (equip it yourself)", name, qty))
		}

		// Guarded hard: on a natively Critical save the player owns this
		// legitimately, and deleting it would be destroying their property.
		if opt.RevokeStartItems && old == Critical && target != Critical {
			if count == 0 {
				continue
			}
			if who := FindEquippedAccessory(out, ItemToAccessory[id]); len(who) > 0 {
				names := ""
				for i, ci := range who {
					if i > 0 {
						names += ", "
					}
					names += CharNames[ci]
				}
				changes = append(changes, fmt.Sprintf("  %s: equipped by %s, kept", name, names))
				continue
			}
			if int(count) != qty {
				changes = append(changes, fmt.Sprintf(
					"  %s: you hold x%d (not the starting x%d), kept", name, count, qty))
				continue
			}
			SetItem(out, id, 0, 0)
			changes = append(changes, fmt.Sprintf("  %s: removed x%d from inventory", name, qty))
		}
	}
	return changes
}

// StartItemState reports what the Critical start items would do for this save:
// whether granting one would add anything, and whether revoking one would take
// anything back. The interface uses it to offer those switches only when they
// would change something, so the rules here have to stay in step with
// swapStartItems, which is the code that actually decides.
func StartItemState(plain []byte) (grantable, revocable bool) {
	for id, qty := range CriticalStartItems {
		count, _ := GetItem(plain, id)
		if count == 0 {
			// Nothing held, so granting adds one and there is none to take.
			grantable = true
			continue
		}
		// Held. Revoking only ever takes back exactly the starting quantity,
		// and never off a character who is wearing it.
		if int(count) == qty && len(FindEquippedAccessory(plain, ItemToAccessory[id])) == 0 {
			revocable = true
		}
	}
	return grantable, revocable
}
