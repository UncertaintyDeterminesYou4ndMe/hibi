package main

import (
	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
)

const panelWidth = 340

// palette holds the panel's colors. Text uses only Ink and Muted; both
// stay at 4.5:1 or more against every background the panel can show
// (Paper, Surface, Soft and the system popover material). Deep and Faint
// are for graphics only and stay at 3:1 or more. design_test.go checks it.
type palette struct {
	Ink, Paper, Surface, Soft ui.Color
	Deep, Muted, Faint        ui.Color
}

var lightPalette = palette{
	Ink: ui.Hex("#0E0E10"), Paper: ui.Hex("#FFFFFF"), Surface: ui.Hex("#F2F2F3"), Soft: ui.Hex("#E5E5E7"),
	Deep: ui.Hex("#333337"), Muted: ui.Hex("#5C5C61"), Faint: ui.Hex("#808085"),
}

var darkPalette = palette{
	Ink: ui.Hex("#F4F4F5"), Paper: ui.Hex("#0A0A0A"), Surface: ui.Hex("#1C1C1D"), Soft: ui.Hex("#272729"),
	Deep: ui.Hex("#CDCDD1"), Muted: ui.Hex("#A6A6AB"), Faint: ui.Hex("#86868B"),
}

func applyPalette(c *ui.Context) palette {
	p := lightPalette
	if c.Theme().Dark {
		p = darkPalette
	}
	t := *c.Theme()
	t.Background = p.Paper
	t.Surface = p.Surface
	t.SurfaceHover = p.Surface
	t.SurfacePressed = p.Soft
	t.Border = p.Soft
	t.Text = p.Ink
	t.TextMuted = p.Muted
	t.Accent = p.Ink
	t.AccentHover = p.Ink
	t.AccentPressed = p.Muted
	t.AccentText = p.Paper
	t.InactiveAccent = p.Ink
	t.Danger = p.Ink
	t.Focus = p.Ink
	t.Selection = p.Surface
	t.Radius = 4
	t.Spacing = 4
	c.SetTheme(&t)
	return p
}

// paperColor keeps the flat paper where no material shows, and lets the
// system popover material show through where it does.
func paperColor(c *ui.Context, p palette) ui.Color {
	if c.Vibrancy() {
		return ui.Transparent
	}
	return p.Paper
}

// glyph draws the system's SF Symbol where macOS has it, else the
// matching self-drawn icon of the same 24px grid.
// glyph draws the SF Symbol name, or fallback where there is none; an empty
// name draws fallback everywhere.
func glyph(c *ui.Context, name string, fallback *ui.SVG, size float32, color ui.Color) ui.Element {
	if name == "" {
		return ui.Icon(c, fallback).Size(size, size).TextColor(color)
	}
	return sfsymbols.Symbol(c, name).Size(size).Color(color).Weight(sfsymbols.Medium).Fallback(fallback).Element
}

func iconButton(c *ui.Context, name string, icon *ui.SVG, label string, p palette) ui.Element {
	b := ui.ButtonBase(c).Label(label).Tooltip(label).Padding(2).Radius(4)
	color := p.Muted
	if b.Hovered() || b.Pressed() {
		color = p.Ink
		b.Background(p.Surface)
	}
	b.Children(func() { glyph(c, name, icon, 13, color) })
	return b
}

func textButton(c *ui.Context, name string, icon *ui.SVG, text, label string, p palette) ui.Element {
	b := ui.ButtonBase(c).Label(label).Tooltip(label).Padding(3, 4).Radius(4)
	color := p.Muted
	if b.Hovered() || b.Pressed() {
		color = p.Ink
		b.Background(p.Surface)
	}
	b.Children(func() {
		ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
			if icon != nil {
				glyph(c, name, icon, 13, color)
			}
			ui.Text(c, text).FontSize(11).TextColor(color)
		})
	})
	return b
}

func rule(c *ui.Context, p palette, height float32) {
	ui.Box(c).FillWidth().Height(height).Background(p.Soft)
}

// quadrantColor marks a task's quadrant: the more important, the darker.
// Every mark stays visible against the row (3:1 or more).
func quadrantColor(q int, p palette) ui.Color {
	switch q {
	case 1:
		return p.Ink
	case 2:
		return p.Deep
	case 3:
		return p.Muted
	case 4:
		return p.Faint
	default:
		return p.Paper.Alpha(0)
	}
}

// taskCard fills a task row: Surface under the pointer, and one step
// darker for the keyboard's selection, so the two read apart.
func taskCard(c *ui.Context, e ui.Element, p palette, hovered, selected bool) ui.Element {
	fill := p.Paper
	if c.Vibrancy() {
		fill = ui.Transparent
	}
	if hovered {
		fill = p.Surface
	}
	if selected {
		fill = p.Soft
	}
	return e.Background(fill).Radius(4)
}
