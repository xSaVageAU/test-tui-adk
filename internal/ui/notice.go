// This file is the transient top-bar status notice: setNotice puts a
// short-lived badge in the center of the top bar (see header.go's
// joinLeftCenterRight) instead of appending a permanent system badge to
// the transcript. The split between the two is deliberate: confirmations
// and progress notes ("Theme set to X.", "Reloading agents...") go here
// and quietly expire, while anything reporting a failure or refusing an
// action stays a transcript systemMessage — an error that vanished after
// a few seconds would be data loss, and a refusal ("wait for the current
// response...") needs to still be readable when the user looks up to see
// why nothing happened.
package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// noticeDuration is how long a status notice stays visible before the
// top bar goes back to just session + context bar.
const noticeDuration = 4 * time.Second

type noticeExpireMsg struct{}

// setNotice shows text in the top bar's status area until it expires (or
// a newer notice replaces it, restarting the clock). Deliberately plain
// (no tea.Cmd returned) so any call site — however deep inside a void
// helper — can use it exactly like systemMessage; the expiry timer is
// scheduled centrally by App.Update's wrapper, the one point every
// update pass funnels through, whenever it sees a live notice with no
// timer running.
func (a *App) setNotice(text string) {
	a.notice = text
	a.noticeExpiry = time.Now().Add(noticeDuration)
}

func noticeExpireTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return noticeExpireMsg{} })
}
