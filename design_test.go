package main

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// relativeLuminance follows WCAG 2.x for an opaque sRGB color.
func relativeLuminance(c ui.Color) float64 {
	channel := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
}

func contrastRatio(a, b ui.Color) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// TestPaletteMeetsWCAGContrast checks WCAG 2.2 AA: text at 4.5:1 (1.4.3)
// and graphics that carry meaning at 3:1 (1.4.11), against every background
// a task row can have. The panel normally shows the system popover
// material, whose color depends on what is behind it; the last background
// of each mode is a deliberately dark (light mode) or light (dark mode)
// bound for it.
func TestPaletteMeetsWCAGContrast(t *testing.T) {
	modes := []struct {
		name        string
		p           palette
		materialEnd ui.Color
	}{
		{"light", lightPalette, ui.Hex("#E5E5E5")},
		{"dark", darkPalette, ui.Hex("#3A3A3A")},
	}
	for _, m := range modes {
		p := m.p
		backgrounds := map[string]ui.Color{
			"paper": p.Paper, "hovered row": p.Surface, "selected row": p.Soft, "material bound": m.materialEnd,
		}
		text := map[string]ui.Color{"Ink": p.Ink, "Muted": p.Muted}
		graphics := map[string]ui.Color{"Faint (circle, plus)": p.Faint}
		for q := 1; q <= 4; q++ {
			graphics["quadrant "+string(rune('0'+q))] = quadrantColor(q, p)
		}
		for bgName, bg := range backgrounds {
			for name, c := range text {
				if r := contrastRatio(c, bg); r < 4.5 {
					t.Errorf("%s: text %s on %s is %.2f:1, want 4.5:1", m.name, name, bgName, r)
				}
			}
			for name, c := range graphics {
				if r := contrastRatio(c, bg); r < 3 {
					t.Errorf("%s: graphic %s on %s is %.2f:1, want 3:1", m.name, name, bgName, r)
				}
			}
		}
	}
}
