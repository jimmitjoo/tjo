package auth

import (
	"context"
	"errors"
)

// Scoping queries to an organization.
//
// The tenancy boundary in this design is a WHERE clause, which is only safe if
// it is never forgotten. Forgetting it does not produce an error -- it produces
// a query that returns every tenant's rows, looks correct in development where
// there is one tenant, and leaks in production. That is the failure mode this
// exists to make harder.

// ErrNoActiveOrganization is returned when a scoped operation is attempted
// without one.
//
// Deliberately an error rather than a silent unscoped query. A helper that
// quietly dropped the filter when no organization was set would be the exact
// bug it was written to prevent.
var ErrNoActiveOrganization = errors.New("auth: no active organization in this context")

type orgContextKey struct{}

// WithOrganization returns a context carrying the active organization.
//
// Set it in middleware, once, from the session. Passing the organization as a
// function argument everywhere is the alternative and it fails the same way a
// forgotten WHERE clause does: the one call site that omits it compiles.
func WithOrganization(ctx context.Context, orgID string) context.Context {
	return context.WithValue(ctx, orgContextKey{}, orgID)
}

// OrganizationFrom returns the active organization.
func OrganizationFrom(ctx context.Context) (string, error) {
	orgID, ok := ctx.Value(orgContextKey{}).(string)
	if !ok || orgID == "" {
		return "", ErrNoActiveOrganization
	}
	return orgID, nil
}

// ScopeTo adds the organization filter to a query builder.
//
// Generic over the builder type so it composes with the fluent API without the
// auth package depending on it:
//
//	qb, err := auth.ScopeTo(ctx, database.NewQueryBuilder(db).Table("invoices"), "organization_id")
//	if err != nil {
//	    return err
//	}
//	rows, err := qb.Where("status", "=", "unpaid").Get()
//
// It returns an error rather than an unscoped builder when no organization is
// active, so the mistake is loud.
func ScopeTo[T any](ctx context.Context, qb interface {
	Where(string, string, interface{}) T
}, column string) (T, error) {
	var zero T

	orgID, err := OrganizationFrom(ctx)
	if err != nil {
		return zero, err
	}
	if column == "" {
		column = "organization_id"
	}

	return qb.Where(column, "=", orgID), nil
}

// MustBelong checks membership before a scoped operation.
//
// Scoping alone answers "which rows", not "may this person see them". An
// account with no membership at all still gets a syntactically valid scoped
// query; this is what makes it an empty one on purpose rather than by accident.
func MustBelong(ctx context.Context, store OrganizationStore, accountID string) (string, error) {
	orgID, err := OrganizationFrom(ctx)
	if err != nil {
		return "", err
	}

	m, err := store.MembershipOf(ctx, orgID, accountID)
	if err != nil {
		return "", err
	}
	if m == nil {
		return "", ErrNotAMember
	}

	return orgID, nil
}
