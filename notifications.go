package main

import (
	"github.com/egoist/mygo/ui"

	"motrix-mygo/engine"
)

func (a *app) notificationsPage(c *ui.Context, p palette) {
	a.unread = 0
	a.pageHeader(c, T("Notifications"), func() {
		ui.Spacer(c)
		if ui.Button(c, T("Clear all")).Disabled(len(a.notes) == 0).Clicked() {
			a.notes = nil
		}
	})
	if len(a.notes) == 0 {
		emptyState(c, p, "bell", T("No notifications"), T("Finished and failed downloads show up here."))
		return
	}
	now := c.Now()
	ui.List(c.Key("notes"), nil, len(a.notes), func(i int) {
		n := a.notes[i]
		failed := n.task.Status == engine.Failed
		ui.Row(c).Padding(12, 24).Gap(14).AlignItems(ui.Start).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
			if failed {
				ui.Icon(c, icon("circle-alert")).FontSize(20).TextColor(c.Theme().Danger).Margin(2, 0, 0, 0)
			} else {
				ui.Icon(c, icon("circle-check")).FontSize(20).TextColor(p.success).Margin(2, 0, 0, 0)
			}
			ui.Column(c).Grow(1).Basis(0).Gap(3).Children(func() {
				title := T("Download completed")
				if failed {
					title = T("Download failed")
				}
				ui.Text(c, title).FontSize(14).FontWeight(600)
				ui.Text(c, displayName(n.task)).FontSize(13).TextColor(p.muted).MaxLines(2)
				if failed {
					ui.Text(c, n.task.Err).FontSize(12.5).TextColor(c.Theme().Danger).MaxLines(3)
				}
			})
			ui.Text(c, formatWhen(n.at, now)).FontSize(12).TextColor(p.muted).FontFeatures("tnum").SingleLine()
		})
	}).Grow(1)
}
