package mail

import (
	"context"
	"net/mail"
	"slices"
	"strings"
	"sync"
)

// Sink is a Sender that keeps what it is given instead of sending it. Tests
// read the code a person was mailed from it, and an application's first run,
// before it has a relay, can use it too.
type Sink struct {
	mu   sync.Mutex
	sent []Message
}

// NewSink returns an empty Sink.
func NewSink() *Sink { return &Sink{} }

// Send records the message after the checks every sender makes.
func (s *Sink) Send(_ context.Context, m Message) error {
	if err := m.Check(); err != nil {
		return err
	}

	m.To = slices.Clone(m.To)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.sent = append(s.sent, m)

	return nil
}

// Sent returns every message, oldest first.
func (s *Sink) Sent() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.sent)
}

// Last returns the latest message, and false when there is none.
func (s *Sink) Last() (Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.sent) == 0 {
		return Message{}, false
	}

	return s.sent[len(s.sent)-1], true
}

// To returns the messages sent to the address (compared without case), oldest first.
func (s *Sink) To(address string) []Message {
	var out []Message

	for _, m := range s.Sent() {
		for _, to := range m.To {
			if sameAddress(to, address) {
				out = append(out, m)

				break
			}
		}
	}

	return out
}

// Reset forgets every message.
func (s *Sink) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sent = nil
}

// sameAddress compares two recipients by their address, ignoring case and any name.
func sameAddress(a, b string) bool {
	pa, errA := mail.ParseAddress(a)
	pb, errB := mail.ParseAddress(b)

	if errA != nil || errB != nil {
		return strings.EqualFold(a, b)
	}

	return strings.EqualFold(pa.Address, pb.Address)
}
