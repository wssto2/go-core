package authz

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/wssto2/go-core/apperr"
)

// Sentinel errors. Errors returned by the public API wrap one of these inside
// an *apperr.AppError, so callers can use errors.Is as well as the HTTP mapping.
var (
	// ErrNoPrincipal means the context carries no authenticated principal.
	ErrNoPrincipal = errors.New("authz: no principal in context")
	// ErrForbidden means no binding grants the requested access.
	ErrForbidden = errors.New("authz: forbidden")
	// ErrEscalation means an actor tried to give away more than it holds.
	ErrEscalation = errors.New("authz: escalation")
	// ErrLastAdmin means a change would remove the actor's own last access to a
	// protected permission.
	ErrLastAdmin = errors.New("authz: would remove last access")
	// ErrUnknownPermission means a permission is not in the catalogue.
	ErrUnknownPermission = errors.New("authz: unknown permission")
	// ErrRoleNotFound is returned by a Store for a role that does not exist.
	ErrRoleNotFound = errors.New("authz: role not found")
	// ErrBindingNotFound is returned by a Store for a binding that does not exist.
	ErrBindingNotFound = errors.New("authz: binding not found")
	// ErrScopeNotFound is returned by a ScopeResolver for a scope that does not exist.
	ErrScopeNotFound = errors.New("authz: scope not found")
	// ErrDuplicateBinding is returned by a Store for a binding that already exists.
	ErrDuplicateBinding = errors.New("authz: binding already exists")
	// ErrRoleInUse is returned when deleting a role that still has bindings.
	ErrRoleInUse = errors.New("authz: role is bound")
	// ErrInvalidScope means a scope does not fit the hierarchy.
	ErrInvalidScope = errors.New("authz: invalid scope")
)

// Reasons attached to the *apperr.AppError values this package returns.
const (
	ReasonForbidden  apperr.Reason = "authz.forbidden"
	ReasonEscalation apperr.Reason = "authz.escalation"
	ReasonLastAdmin  apperr.Reason = "authz.last_admin"
	ReasonInvalid    apperr.Reason = "authz.invalid"
	ReasonRoleInUse  apperr.Reason = "authz.role_in_use"
)

func forbidden(permission string) error {
	return apperr.Wrap(ErrForbidden, "access denied", apperr.CodePermissionDenied).
		WithLog(apperr.LevelWarn).
		WithReason(ReasonForbidden, map[string]any{"permission": permission})
}

func unauthenticated() error {
	return apperr.Wrap(ErrNoPrincipal, "user not authenticated", apperr.CodeUnauthenticated).
		WithLog(apperr.LevelWarn)
}

func escalation(permission, why string) error {
	return apperr.Wrap(fmt.Errorf("%w: %s: %s", ErrEscalation, permission, why),
		"cannot grant more than you hold", apperr.CodePermissionDenied).
		WithLog(apperr.LevelWarn).
		WithReason(ReasonEscalation, map[string]any{"permission": permission, "why": why})
}

// ProblemCode identifies one kind of role or catalogue problem.
type ProblemCode string

// Problem codes reported by Role.Validate and Catalogue.Validate.
const (
	ProblemUnknownPermission    ProblemCode = "unknown_permission"
	ProblemUnknownAttribute     ProblemCode = "unknown_attribute"
	ProblemEmptyAttribute       ProblemCode = "empty_attribute"
	ProblemInvalidQualifier     ProblemCode = "invalid_qualifier"
	ProblemQualifierNotAllowed  ProblemCode = "qualifier_not_allowed"
	ProblemMissingRequired      ProblemCode = "missing_required"
	ProblemRequiredTooNarrow    ProblemCode = "required_too_narrow"
	ProblemDuplicateGrant       ProblemCode = "duplicate_grant"
	ProblemComputedWithGrants   ProblemCode = "computed_with_grants"
	ProblemInvalidRole          ProblemCode = "invalid_role"
	ProblemUnknownRequirement   ProblemCode = "unknown_requirement"
	ProblemRequirementCycle     ProblemCode = "requirement_cycle"
	ProblemOrganizationOnly     ProblemCode = "organization_only"
	ProblemInvalidIdentifier    ProblemCode = "invalid_identifier"
	ProblemDuplicatePermission  ProblemCode = "duplicate_permission"
	ProblemInconsistentMetadata ProblemCode = "inconsistent_metadata"
)

// Problem is one thing wrong with a role, a binding or a catalogue.
type Problem struct {
	Code       ProblemCode `json:"code"`
	Permission string      `json:"permission,omitempty"`
	Detail     string      `json:"detail,omitempty"`
}

func (p Problem) String() string {
	s := string(p.Code)
	if p.Permission != "" {
		s += " " + p.Permission
	}
	if p.Detail != "" {
		s += " (" + p.Detail + ")"
	}
	return s
}

// ValidationError lists every problem found, so an editor can show them all at
// once. It maps to a 400 through apperr.
type ValidationError struct {
	Problems []Problem
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		parts[i] = p.String()
	}
	return "authz: invalid: " + strings.Join(parts, "; ")
}

// Has reports whether the error contains a problem with the given code.
func (e *ValidationError) Has(code ProblemCode) bool {
	for _, p := range e.Problems {
		if p.Code == code {
			return true
		}
	}
	return false
}

// ErrorReason implements apperr.Reasoner.
func (e *ValidationError) ErrorReason() (string, map[string]any) {
	codes := make([]string, 0, len(e.Problems))
	for _, p := range e.Problems {
		codes = append(codes, string(p.Code))
	}
	sort.Strings(codes)
	return string(ReasonInvalid), map[string]any{"problems": codes}
}

// asValidation returns nil when there are no problems.
func asValidation(problems []Problem) error {
	if len(problems) == 0 {
		return nil
	}
	return &ValidationError{Problems: problems}
}

// Deny returns the error Require gives for a permission that is not granted.
// Fakes use it so their failures look like the real ones.
func Deny(permission string) error { return forbidden(permission) }
