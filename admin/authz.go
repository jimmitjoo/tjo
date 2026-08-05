package admin

import (
	"context"
	"errors"
	"net/http"

	"github.com/jimmitjoo/tjo/auth"
)

// Action is what a request is trying to do.
type Action string

const (
	ActionList   Action = "list"
	ActionView   Action = "view"
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionDelete Action = "delete"
)

// The two ways to refuse, and the difference between them is what a visitor
// already knows.
//
// ErrUnauthenticated becomes 404: nobody has identified themselves, so the
// answer must not confirm that this path is an admin panel, that a resource by
// that name exists, or that the record they guessed at is real. An admin panel
// that announces itself has told a scanner where to point the credential
// stuffing.
//
// ErrForbidden becomes 403: this is a known account being told no. Hiding the
// panel from someone already looking at it buys nothing and produces support
// tickets about pages that "disappeared".
var (
	ErrUnauthenticated = errors.New("admin: not signed in")
	ErrForbidden       = errors.New("admin: not permitted")
)

// Query is one authorization question.
//
// Record is nil for a list or a create -- there is no row yet. Field is set
// only when the question is about one column, which is what makes per-field
// permissions possible without a second interface.
type Query struct {
	Action   Action
	Resource string
	Field    string
	Record   map[string]any
}

// Context is what an authorizer and a bulk action are given.
type Context struct {
	context.Context

	// Request is the HTTP request behind this, for a session lookup or a
	// header. Never nil.
	Request *http.Request
}

// Authorizer decides who may do what.
//
// There is no default that allows anything. A panel without an authorizer
// refuses every request, because the failure mode of the other default is a
// CRUD interface to the whole database published to the internet.
type Authorizer interface {
	Allow(ctx Context, q Query) error
}

// AuthorizerFunc adapts a function.
type AuthorizerFunc func(ctx Context, q Query) error

func (f AuthorizerFunc) Allow(ctx Context, q Query) error { return f(ctx, q) }

// DenyAll refuses everything, invisibly. It is the zero-configuration default.
var DenyAll Authorizer = AuthorizerFunc(func(Context, Query) error {
	return ErrUnauthenticated
})

// AllowAll permits everything.
//
// For local development and for tests. Naming it this loudly is deliberate:
// it should be obvious in a diff, and it should be obvious in a review that a
// production configuration containing it is a finding.
var AllowAll Authorizer = AuthorizerFunc(func(Context, Query) error { return nil })

// Organization is how a request's account and organization are found.
//
// The panel cannot know where an application keeps them -- session, JWT, mTLS
// certificate -- so it asks. Return ("", "", ErrForbidden) for an
// unauthenticated request.
type Organization func(ctx Context) (orgID, accountID string, err error)

// RoleAuthorizer permits actions by the account's role in its organization,
// using the auth package's membership lookup.
//
// The lookup happens on every request rather than being read from the session.
// A session carrying the role keeps granting it after the person has been
// removed, until they happen to log out -- which is the difference between
// revocation and a suggestion.
//
// required maps an action to the permission it needs. Actions missing from the
// map are refused, so adding an action to this package cannot silently widen
// what an existing deployment allows.
func RoleAuthorizer(store auth.OrganizationStore, perms auth.Permissions, who Organization, required map[Action]auth.Permission) Authorizer {
	return AuthorizerFunc(func(ctx Context, q Query) error {
		// Who first, so an anonymous request is invisible rather than
		// informative: an unmapped action would otherwise answer 403 and
		// confirm the panel to someone who never signed in.
		orgID, accountID, err := who(ctx)
		if err != nil || orgID == "" || accountID == "" {
			return ErrUnauthenticated
		}

		permission, ok := required[q.Action]
		if !ok {
			return ErrForbidden
		}

		if err := auth.Authorize(ctx, store, perms, orgID, accountID, permission); err != nil {
			return ErrForbidden
		}
		return nil
	})
}

// DefaultPermissions is the mapping most applications want: reading needs read,
// everything that writes needs write.
func DefaultPermissions() map[Action]auth.Permission {
	return map[Action]auth.Permission{
		ActionList:   auth.PermRead,
		ActionView:   auth.PermRead,
		ActionCreate: auth.PermWrite,
		ActionUpdate: auth.PermWrite,
		ActionDelete: auth.PermWrite,
	}
}

// allow asks the authorizer and reports whether it said yes.
func (p *Panel) allow(ctx Context, q Query) error {
	if p.authorizer == nil {
		return ErrUnauthenticated
	}
	return p.authorizer.Allow(ctx, q)
}

// allowField reports whether one column may be read or written.
//
// Asked per column, per request. An authorizer that does not care about fields
// ignores Query.Field and answers the same as for the record, which is why this
// costs applications that do not need it exactly nothing.
func (p *Panel) allowField(ctx Context, action Action, resource string, field string, record map[string]any) bool {
	return p.allow(ctx, Query{Action: action, Resource: resource, Field: field, Record: record}) == nil
}
