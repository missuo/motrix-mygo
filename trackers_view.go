package main

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

func (a *app) trackersPage(c *ui.Context, p palette) {
	a.pageHeader(c, T("Trackers"), func() {
		ui.Textf(c, "%d", len(a.trackers.Trackers)).FontSize(12.5).FontWeight(600).FontFeatures("tnum").
			Padding(2, 9).Radius(11).Border(1, p.border).SingleLine()
		ui.Spacer(c)
		synced := T("Never updated")
		if !a.trackers.SyncedAt.IsZero() {
			synced = T("Last updated") + " " + a.trackers.SyncedAt.Format("2006-01-02 15:04")
		}
		ui.Text(c, synced).FontSize(12.5).TextColor(p.muted).SingleLine()
		if a.syncing {
			ui.Spinner(c)
		}
		if ui.Button(c, T("Update now")).Disabled(a.syncing).Clicked() {
			a.syncTrackers()
		}
	})
	ui.Divider(c).Background(p.border)
	ui.Scroll(c).Grow(1).Padding(4, 24, 24, 24).Children(func() {
		ui.Column(c).MaxWidth(760).Children(func() {
			sectionTitle(c, p, T("Sources"))
			for i := range a.set.Tracker.Sources {
				src := &a.set.Tracker.Sources[i]
				ui.Row(c.Key(src.ID)).Padding(12, 0).Gap(16).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
					ui.Column(c).Grow(1).Basis(0).Gap(3).Children(func() {
						ui.Text(c, src.Label).FontSize(14).FontWeight(500).SingleLine()
						ui.Text(c, src.URL).FontSize(12).TextColor(p.muted).SingleLine().Selectable()
						if msg := a.trackers.Errors[src.Label]; msg != "" && src.Enabled {
							ui.Text(c, msg).FontSize(12).TextColor(c.Theme().Danger).MaxLines(2)
						}
					})
					if !src.Builtin {
						if ui.Button(c, T("Remove")).Clicked() {
							id := src.ID
							a.set.Tracker.Sources = slices.DeleteFunc(slices.Clone(a.set.Tracker.Sources), func(s trackerSource) bool { return s.ID == id })
							a.set.save(a.paths.settings)
							return
						}
					}
					if ui.Switch(c, &src.Enabled).Label(src.Label).Changed() {
						a.set.save(a.paths.settings)
					}
				})
			}
			ui.Row(c).Padding(14, 0).Gap(8).Children(func() {
				in := ui.TextInput(c.Key("new-source"), &a.newSource).Label(T("Add source")).
					Placeholder("https://example.com/trackers.txt").Grow(1)
				if ui.Button(c, T("Add source")).Clicked() || in.Submitted() {
					a.addTrackerSource()
				}
			})

			sectionTitle(c, p, fmt.Sprintf("%s (%d)", T("Tracker list"), len(a.trackers.Trackers)))
			if len(a.trackers.Trackers) == 0 {
				ui.Text(c, T("No trackers yet. Update the list to add them to new BitTorrent tasks.")).
					FontSize(13).TextColor(p.muted).Padding(12, 0)
				return
			}
			ui.Column(c).Selectable().Padding(10, 0).Gap(5).Children(func() {
				for _, tracker := range a.trackers.Trackers {
					ui.Text(c, tracker).FontSize(12).Font("monospace").TextColor(p.muted).SingleLine()
				}
			})
		})
	})
}

// addTrackerSource adds the URL typed as a source of trackers.
func (a *app) addTrackerSource() {
	link := strings.TrimSpace(a.newSource)
	u, err := url.Parse(link)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		a.flash = T("Enter the HTTP or HTTPS link of a tracker list.")
		return
	}
	if slices.ContainsFunc(a.set.Tracker.Sources, func(s trackerSource) bool { return s.URL == link }) {
		a.newSource = ""
		return
	}
	a.set.Tracker.Sources = append(a.set.Tracker.Sources, trackerSource{
		ID: "custom-" + link, Label: u.Host + u.Path, URL: link, Enabled: true,
	})
	a.newSource = ""
	a.set.save(a.paths.settings)
}
