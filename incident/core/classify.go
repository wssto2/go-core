package core

import "slices"

// Valid reports whether s is a known status.
//
// INC-CLASSIFY-001.
func (s Status) Valid() bool {
	return slices.Contains(
		[]Status{StatusNew, StatusTriaged, StatusInProgress, StatusResolved, StatusIgnored, StatusDuplicate}, s,
	)
}

// IsOpen reports whether an incident in this status is still being worked, as opposed to resolved, ignored or a
// duplicate. An open incident keeps its OpenFingerprint, so a new report or occurrence with the same fingerprint is
// merged into it instead of starting a fresh incident.
//
// INC-CLASSIFY-001.
func (s Status) IsOpen() bool {
	return slices.Contains([]Status{StatusNew, StatusTriaged, StatusInProgress}, s)
}

// Valid reports whether s is a known severity.
//
// INC-CLASSIFY-001.
func (s Severity) Valid() bool {
	return slices.Contains([]Severity{SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical}, s)
}

// OrDefault returns s when it is a known severity, else SeverityMedium: a severity a caller may leave out or get wrong
// (a person's bug report).
//
// INC-CLASSIFY-001.
func (s Severity) OrDefault() Severity {
	if s.Valid() {
		return s
	}

	return SeverityMedium
}

// Merge combines the source an incident has with an incoming one: the same source stays, an empty source adopts the
// incoming one, and two different sources become SourceMixed, an incident seen from several sources (a person reports
// a bug a runtime error already opened an incident for).
//
// INC-CLASSIFY-001.
func (s Source) Merge(incoming Source) Source {
	if s == "" {
		return incoming
	}

	if s == incoming {
		return s
	}

	return SourceMixed
}
