package authz

import "fmt"

// Qualifier says whose records a grant of an ownable permission reaches.
//
// All reaches every record in the scope, so it covers the other two. Own and
// OwnLocation are NOT nested: a record the principal owns may sit at another
// location, and a record at the principal's location may be someone else's.
// Neither covers the other (see Covers). The numeric order (Own < OwnLocation <
// All) is only a display ranking, used by AccessSet.Widest and MyAccess.
type Qualifier int

const (
	// QualifierOwn reaches records the principal owns (plus unowned records, for
	// permissions declared with UnownedIsOwn).
	QualifierOwn Qualifier = iota + 1
	// QualifierOwnLocation reaches records of the principal's own location.
	QualifierOwnLocation
	// QualifierAll reaches every record within the binding's scope. It is the
	// only qualifier a non-ownable permission may carry.
	QualifierAll
)

// String returns "own", "own_location" or "all" ("invalid" for anything else).
func (q Qualifier) String() string {
	switch q {
	case QualifierOwn:
		return "own"
	case QualifierOwnLocation:
		return "own_location"
	case QualifierAll:
		return "all"
	}
	return "invalid"
}

// Valid reports whether q is one of the three qualifiers.
func (q Qualifier) Valid() bool { return q >= QualifierOwn && q <= QualifierAll }

// Covers reports whether every record other reaches is also reached by q: All
// covers every qualifier, and Own and OwnLocation cover only themselves.
func (q Qualifier) Covers(other Qualifier) bool { return q == QualifierAll || q == other }

// ParseQualifier is the inverse of String.
func ParseQualifier(s string) (Qualifier, error) {
	for _, q := range []Qualifier{QualifierOwn, QualifierOwnLocation, QualifierAll} {
		if q.String() == s {
			return q, nil
		}
	}
	return 0, fmt.Errorf("authz: unknown qualifier %q", s)
}

// MarshalText makes a Qualifier a JSON string.
func (q Qualifier) MarshalText() ([]byte, error) {
	if !q.Valid() {
		return nil, fmt.Errorf("authz: invalid qualifier %d", int(q))
	}
	return []byte(q.String()), nil
}

// UnmarshalText parses the JSON string form.
func (q *Qualifier) UnmarshalText(b []byte) error {
	v, err := ParseQualifier(string(b))
	if err != nil {
		return err
	}
	*q = v
	return nil
}
