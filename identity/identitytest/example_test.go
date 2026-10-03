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
