package core

// Valid reports whether s is a known status.
//
// INC-CLASSIFY-001.
func (s Status) Valid() bool {
	switch s {
	case StatusNew, StatusTriaged, StatusInProgress, StatusResolved, StatusIgnored, StatusDuplicate:
		return true
	default:
		return false
	}
}

// IsOpen reports whether an incident in this status is still being worked, as opposed to resolved, ignored or a
// duplicate. An open incident keeps its OpenFingerprint, so a new report or occurrence with the same fingerprint is
// merged into it instead of starting a fresh incident.
//
// INC-CLASSIFY-001.
func (s Status) IsOpen() bool {
	switch s {
	case StatusNew, StatusTriaged, StatusInProgress:
		return true
	default:
		return false
	}
}

// Valid reports whether s is a known severity.
//
// INC-CLASSIFY-001.
func (s Severity) Valid() bool {
	switch s {
	case SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical:
		return true
	default:
		return false
	}
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
