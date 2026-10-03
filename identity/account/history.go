package account

import "slices"

// SignInView is which rows of a sign-in history are shown: the tabs of the page.
type SignInView string

// The views of a sign-in history.
const (
	// SignInsAll is every row; the empty view is the same.
	SignInsAll SignInView = "all"
	// SignInsFailed is the refused attempts: a wrong password, a refusal while locked and one
	// for an inactive account.
	SignInsFailed SignInView = "failed"
)

// failedEvents are the events SignInsFailed shows.
var failedEvents = []SignInEvent{WrongPassword, LockedOut, RefusedInactive}

// SignInCounts is how many rows each view of a history has.
type SignInCounts struct {
	All, Failed int
}

// SignInPage is a page of a sign-in history, newest first: the rows, how many rows the view has,
// and how many each view has (whichever is shown).
type SignInPage struct {
	Rows   []SignInEntry
	Total  int
	Counts SignInCounts
}

// events are the events of the view: none for the view that shows every one, and the reason when
// the view does not exist.
func (v SignInView) events() ([]SignInEvent, error) {
	switch v {
	case "", SignInsAll:
		return nil, nil
	case SignInsFailed:
		return failedEvents, nil
	}

	return nil, invalid("view", ReasonHistoryViewInvalid)
}

func signInCounts(byEvent map[SignInEvent]int) SignInCounts {
	var out SignInCounts

	for event, n := range byEvent {
		out.All += n

		if slices.Contains(failedEvents, event) {
			out.Failed += n
		}
	}

	return out
}

// ChangeView is which changes to an account are shown: the tabs of the page.
type ChangeView string

// The views of the changes made to an account.
const (
	// ChangesAll is every change; the empty view is the same.
	ChangesAll ChangeView = "all"
	// ChangesAccess is what decides whether and how the person can get in: a new password, a
	// deactivation and an activation.
	ChangesAccess ChangeView = "access"
	// ChangesDetails is everything else: the account's creation and the changes to its details
	// and e-mail address.
	ChangesDetails ChangeView = "details"
)

// accessActions are the actions ChangesAccess shows; ChangesDetails is every other.
var accessActions = []ChangeAction{ChangePassword, ChangeDeactivated, ChangeActivated}

// ChangeCounts is how many changes each view of an account's changes has.
type ChangeCounts struct {
	All, Access, Details int
}

// ChangePage is a page of the changes made to an account, newest first: the rows, how many the
// view has, and how many each view has (whichever is shown).
type ChangePage struct {
	Rows   []ChangeEntry
	Total  int
	Counts ChangeCounts
}

// narrow says which actions the view shows (only) or leaves out (except), and the reason when the
// view does not exist.
func (v ChangeView) narrow() (only, except []ChangeAction, err error) {
	switch v {
	case "", ChangesAll:
		return nil, nil, nil
	case ChangesAccess:
		return accessActions, nil, nil
	case ChangesDetails:
		return nil, accessActions, nil
	}

	return nil, nil, invalid("view", ReasonHistoryViewInvalid)
}

func changeCounts(byAction map[ChangeAction]int) ChangeCounts {
	var out ChangeCounts

	for action, n := range byAction {
		out.All += n

		if slices.Contains(accessActions, action) {
			out.Access += n
		}
	}

	out.Details = out.All - out.Access

	return out
}
