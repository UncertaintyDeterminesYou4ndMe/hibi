package main

import "github.com/egoist/mygo/ui"

// All icons use one 24px grid, a 1.8px stroke and rounded line endings.
type iconSet struct {
	play, pause, stop, plus, file, help, quit, timer, check, trash,
	chevron, cat, catAsleep, close, arrow, sliders *ui.SVG
}

var icons = iconSet{
	play:    svgIcon(`<path d="M8 5v14l11-7z" fill="currentColor" stroke="none"/>`),
	pause:   svgIcon(`<path d="M7 5h3v14H7zM14 5h3v14h-3z" fill="currentColor" stroke="none"/>`),
	stop:    svgIcon(`<rect x="6" y="6" width="12" height="12"/>`),
	plus:    svgIcon(`<path d="M12 5v14M5 12h14"/>`),
	file:    svgIcon(`<path d="M6 3h8l4 4v14H6zM14 3v5h5M9 12h6M9 16h6"/>`),
	help:    svgIcon(`<circle cx="12" cy="12" r="9"/><path d="M9.7 9a2.4 2.4 0 0 1 4.6 1c0 2-2.3 2.2-2.3 4M12 17h.01"/>`),
	quit:    svgIcon(`<path d="M10 4H5v16h5M13 8l4 4-4 4M8 12h9"/>`),
	timer:   svgIcon(`<circle cx="12" cy="13" r="8"/><path d="M12 9v4l3 2M9 3h6M12 3v2"/>`),
	check:   svgIcon(`<path d="M5 12l4 4L19 6"/>`),
	trash:   svgIcon(`<path d="M5 7h14M9 7V4h6v3M7 7l1 13h8l1-13M10 10v6M14 10v6"/>`),
	chevron: svgIcon(`<path d="M6 9l6 6 6-6"/>`),
	// cat is a cat's head: a filled round face with rounded ears, the eyes
	// and nose cut out, and short whiskers either side.
	cat: svgIcon(`<path fill="currentColor" stroke="none" fill-rule="evenodd" d="M3 14.6c0-2 .6-3.8 1.5-5L4.2 5.4c-.1-.9.6-1.3 1.3-.9l3.6 2.3c.9-.25 1.9-.4 2.9-.4s2 .15 2.9.4l3.6-2.3c.7-.4 1.4 0 1.3.9l-.3 4.2c.9 1.2 1.5 3 1.5 5 0 3.6-3.8 5.9-9 5.9s-9-2.3-9-5.9zM7.7 14.4a1.15 1.35 0 1 0 2.3 0a1.15 1.35 0 1 0-2.3 0zM14 14.4a1.15 1.35 0 1 0 2.3 0a1.15 1.35 0 1 0-2.3 0zM11.2 16.8h1.6l-.8.9z"/><path stroke-width="1.2" d="M.7 14.6l1.7.4M.9 17.4l1.7-.6M23.3 14.6l-1.7.4M23.1 17.4l-1.7-.6"/>`),
	// catAsleep is the same head with closed lids curving down, for a break.
	catAsleep: svgIcon(`<path fill="currentColor" stroke="none" fill-rule="evenodd" d="M3 14.6c0-2 .6-3.8 1.5-5L4.2 5.4c-.1-.9.6-1.3 1.3-.9l3.6 2.3c.9-.25 1.9-.4 2.9-.4s2 .15 2.9.4l3.6-2.3c.7-.4 1.4 0 1.3.9l-.3 4.2c.9 1.2 1.5 3 1.5 5 0 3.6-3.8 5.9-9 5.9s-9-2.3-9-5.9zM6.596 14.15A2.414 2.414 0 0 0 11.104 14.15A.7 .7 0 0 0 9.797 13.649A1.014 1.014 0 0 1 7.903 13.649A.7 .7 0 0 0 6.596 14.15zM12.896 14.15A2.414 2.414 0 0 0 17.404 14.15A.7 .7 0 0 0 16.097 13.649A1.014 1.014 0 0 1 14.203 13.649A.7 .7 0 0 0 12.896 14.15zM11.2 16.8h1.6l-.8.9z"/><path stroke-width="1.2" d="M.7 14.6l1.7.4M.9 17.4l1.7-.6M23.3 14.6l-1.7.4M23.1 17.4l-1.7-.6"/>`),
	close:     svgIcon(`<path d="M6 6l12 12M18 6L6 18"/>`),
	arrow:     svgIcon(`<path d="M5 12h14M13 6l6 6-6 6"/>`),
	sliders:   svgIcon(`<path d="M4 7h10M18 7h2M4 17h4M12 17h8"/><circle cx="16" cy="7" r="2"/><circle cx="10" cy="17" r="2"/>`),
}

func svgIcon(shapes string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">` + shapes + `</svg>`))
}
