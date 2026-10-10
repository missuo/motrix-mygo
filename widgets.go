package main

import (
	"github.com/egoist/mygo/ui"
)

// iconButton is a round button showing only an icon, named by label for
// assistive technology and its tooltip.
func iconButton(c *ui.Context, p palette, name, label string) ui.Element {
	b := ui.ButtonBase(c).Size(32, 32).Radius(16).Center().Label(label).Tooltip(label).TextColor(p.text)
	if b.Hovered() {
		b.Background(p.hover)
	}
	return b.Children(func() { ui.Icon(c, icon(name)).FontSize(17) })
}

// pill is a rounded box of the status bar and the page headers.
func pill(c *ui.Context, p palette) ui.Element {
	return ui.Row(c).Height(28).Padding(0, 12).Gap(7).Radius(14).AlignItems(ui.Center).
		Background(p.hover).FontSize(13).FontWeight(500)
}

// dot is a small round light, as of the engine's state.
func dot(c *ui.Context, color ui.Color) ui.Element {
	return ui.Box(c).Size(8, 8).Radius(4).Background(color)
}

// caption is the small capital label that heads a tile.
func caption(c *ui.Context, p palette, s string) ui.Element {
	return ui.Text(c, s).FontSize(11).FontWeight(600).LetterSpacing(0.8).TextColor(p.muted).SingleLine()
}

// emptyState is a title over a line of explanation, centered in the room
// its parent leaves.
func emptyState(c *ui.Context, p palette, iconName, title, detail string) {
	ui.Column(c).Grow(1).Center().Gap(10).Padding(32).Children(func() {
		if iconName != "" {
			ui.Icon(c, icon(iconName)).FontSize(34).TextColor(p.muted).Margin(0, 0, 6, 0)
		}
		ui.Text(c, title).FontSize(19).FontWeight(600).TextAlign(ui.Center)
		ui.Text(c, detail).FontSize(14.5).TextColor(p.muted).TextAlign(ui.Center).MaxWidth(420).LineHeight(1.45)
	})
}
