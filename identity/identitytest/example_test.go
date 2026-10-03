package identitytest_test

import (
	"context"
	"fmt"

	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/identitytest"
)

func ExampleMailbox() {
	box := &identitytest.Mailbox{}

	_ = box.SendCode(context.Background(), account.CodeMessage{Recipient: "ana@example.test", Code: "123456"})

	fmt.Println(box.Last().Recipient, box.Last().Code, len(box.Sent()))
	// Output: ana@example.test 123456 1
}

func ExampleNewActivityLog() {
	clock := identitytest.NewClock(identitytest.Epoch)
	changes, sessions := identitytest.NewChangeLog(clock), identitytest.NewSessions()
	log := identitytest.NewActivityLog(changes, sessions)

	_ = changes.Record(context.Background(), account.Change{AccountID: 2, ActorID: 1, Action: account.ChangeCreated})
	_ = changes.Record(context.Background(), account.Change{AccountID: 2, ActorID: 1, Action: account.ChangePassword})

	rows, total, _ := log.Activity(context.Background(), account.ActivityQuery{ActorID: 1, Limit: 10})
	for _, r := range rows {
		fmt.Println(r.RecordType, r.RecordID, r.Action)
	}

	fmt.Println(total)
	// Output:
	// account 2 changed
	// account 2 created
	// 2
}
