package main

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
)

// checking says that a check the user asked for is under way.
var checking atomic.Bool

// checkForUpdates checks for a new version as the user asked. What there is
// to say without one, that the app is up to date or that the check failed,
// it says in an alert of the system, as Sparkle does; a new version goes to
// the update window, with its release notes.
func checkForUpdates() {
	if !checking.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer checking.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		up, err := mygo.Updater.Check(ctx)
		switch {
		case errors.Is(err, mygo.ErrUpdatesDisabled):
			detail := T("Motrix cannot update itself where it is installed. If a package manager installed it, update it there.")
			if mygo.IsDev() {
				detail = T("Development builds do not update themselves.")
			}
			alert(T("Cannot update"), detail)
		case err != nil:
			alert(T("Update Error!"), T("An error occurred while checking for updates. Please try again later."))
		case up == nil:
			alert(T("You’re up to date!"), fmt.Sprintf(T("Motrix %s is currently the newest version available."), mygo.App.Version()))
		default:
			updater.CheckForUpdates()
		}
	}()
}

// alert shows a message of the system with the app's icon and an OK button.
func alert(message, detail string) {
	mygo.Dialog.Message(mygo.MessageOptions{Message: message, Detail: detail, Buttons: []string{T("OK")}})
}
