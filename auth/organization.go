package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Organizations, and why multi-tenancy lives here rather than in a tenancy
// subsystem.
//
// Every other ecosystem grew a dedicated tenancy package -- stancl/tenancy for
// Laravel, django-tenants for Django -- and they are complicated because they
// solve database-per-tenant and domain-per-tenant routing. Most applications
// need neither. What they need is: which organization is this request acting
// as, is this person a member, and what may they do.
//
// All three are session questions, which makes them auth questions. Once the
// session knows the active organization, scoping is a WHERE clause rather than
// a connection-routing problem, and the machinery that makes the PHP and Python
// equivalents heavy never has to exist.
//
// Deliberately absent: database-per-tenant, schema-per-tenant, domain routing.
// If someone brings a real case they can be added; building them first would be
// building the complicated half for a need nobody has stated.

var (
	// ErrNotAMember is returned when an account is not in an organization.
	ErrNotAMember = errors.New("auth: not a member of this organization")

	// ErrForbidden is returned when a member lacks the required permission.
	ErrForbidden = errors.New("auth: insufficient permission")

	// ErrInvitationInvalid covers unknown, expired and already-accepted
	// invitations, for the same reason ErrInvalidReset covers three cases:
	// distinguishing them lets someone probe which invitations exist.
	ErrInvitationInvalid = errors.New("auth: invalid or expired invitation")
)

// Role is a named set of permissions within an organization.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

// Permission is an action within an organization.
type Permission string

const (
	PermManageMembers Permission = "members:manage"
	PermManageBilling Permission = "billing:manage"
	PermManageOrg     Permission = "org:manage"
	PermDeleteOrg     Permission = "org:delete"
	PermRead          Permission = "read"
	PermWrite         Permission = "write"
)

// Permissions maps roles to what they may do.
//
// A map rather than a hierarchy: "admin implies member" reads well and then
// someone adds a role that is not on the line and the model stops describing
// reality. Listing them is longer and stays true.
type Permissions map[Role][]Permission

// DefaultPermissions is the three-role model almost every application starts
// with. Callers add roles by extending it, which is why it returns a fresh map.
func DefaultPermissions() Permissions {
	return Permissions{
		RoleOwner:  {PermManageMembers, PermManageBilling, PermManageOrg, PermDeleteOrg, PermRead, PermWrite},
		RoleAdmin:  {PermManageMembers, PermManageOrg, PermRead, PermWrite},
		RoleMember: {PermRead, PermWrite},
	}
}

// Allows reports whether role may perform permission.
func (p Permissions) Allows(role Role, permission Permission) bool {
	return slices.Contains(p[role], permission)
}

// Membership ties an account to an organization with a role.
type Membership struct {
	OrganizationID string
	AccountID      string
	Role           Role
	JoinedAt       time.Time
}

// Invitation is a single-use, expiring offer of membership.
//
// It reuses the reset-token design rather than inventing a second one: the
// plaintext goes in the emailed link, only the hash is stored, and acceptance
// is atomic. An invitation that can be accepted twice creates two memberships,
// and an invitation whose token is stored in plaintext is a membership anyone
// with database access can grant themselves.
type Invitation struct {
	PlainText      string
	Hash           []byte
	OrganizationID string
	Email          string
	Role           Role
	InvitedBy      string
	Expiry         time.Time
}

// NewInvitation mints an invitation.
func NewInvitation(orgID, email string, role Role, invitedBy string, ttl time.Duration) (*Invitation, error) {
	if orgID == "" || email == "" {
		return nil, errors.New("auth: invitation needs an organization and an email")
	}

	tok, err := NewResetToken(orgID, PurposeActivation, ttl)
	if err != nil {
		return nil, err
	}

	return &Invitation{
		PlainText:      tok.PlainText,
		Hash:           tok.Hash,
		OrganizationID: orgID,
		Email:          strings.ToLower(strings.TrimSpace(email)),
		Role:           role,
		InvitedBy:      invitedBy,
		Expiry:         tok.Expiry,
	}, nil
}

// OrganizationStore is the storage the caller provides.
type OrganizationStore interface {
	// MembershipOf returns the membership, or (nil, nil) when there is none.
	MembershipOf(ctx context.Context, orgID, accountID string) (*Membership, error)

	// MembershipsFor returns every organization an account belongs to.
	MembershipsFor(ctx context.Context, accountID string) ([]*Membership, error)

	// AddMember creates a membership. It must be idempotent or reject
	// duplicates; two memberships for one account in one organization is a
	// state nothing else here handles.
	AddMember(ctx context.Context, m *Membership) error

	// RemoveMember deletes a membership.
	RemoveMember(ctx context.Context, orgID, accountID string) error

	// SaveInvitation persists an invitation. Only the hash is stored.
	SaveInvitation(ctx context.Context, inv *Invitation) error

	// ConsumeInvitation atomically finds an unaccepted, unexpired invitation
	// by hash and marks it accepted.
	//
	// Atomic for the same reason ResetStore.Consume is: a read followed by a
	// write lets two requests both accept, and two memberships is a state the
	// rest of this package does not expect.
	ConsumeInvitation(ctx context.Context, hash []byte) (*Invitation, error)

	// CountOwners returns how many owners an organization has.
	CountOwners(ctx context.Context, orgID string) (int, error)
}

// Authorize reports whether an account may perform a permission in an
// organization.
//
// The membership lookup happens on every call rather than being cached in the
// session. A session that carried the role would keep granting it after the
// person was removed from the organization, until they happened to log out --
// which is the difference between revocation and a suggestion.
func Authorize(ctx context.Context, store OrganizationStore, perms Permissions, orgID, accountID string, permission Permission) error {
	m, err := store.MembershipOf(ctx, orgID, accountID)
	if err != nil {
		return err
	}
	if m == nil {
		return ErrNotAMember
	}
	if !perms.Allows(m.Role, permission) {
		return ErrForbidden
	}
	return nil
}

// AcceptInvitation consumes an invitation and creates the membership.
//
// The email on the invitation is checked against the accepting account's
// address. Without it, anyone who obtains the link -- a forwarded email, a
// shared inbox, a leaked notification -- joins the organization, which turns an
// invitation into a bearer token for membership.
func AcceptInvitation(ctx context.Context, store OrganizationStore, plainToken, accountID, accountEmail string) (*Membership, error) {
	if plainToken == "" || accountID == "" {
		return nil, ErrInvitationInvalid
	}

	inv, err := store.ConsumeInvitation(ctx, HashResetToken(plainToken))
	if err != nil || inv == nil {
		return nil, ErrInvitationInvalid
	}

	if !time.Now().Before(inv.Expiry) {
		return nil, ErrInvitationInvalid
	}

	if !strings.EqualFold(strings.TrimSpace(accountEmail), inv.Email) {
		return nil, ErrInvitationInvalid
	}

	m := &Membership{
		OrganizationID: inv.OrganizationID,
		AccountID:      accountID,
		Role:           inv.Role,
		JoinedAt:       time.Now().UTC(),
	}
	if err := store.AddMember(ctx, m); err != nil {
		return nil, err
	}

	return m, nil
}

// RemoveMember removes someone, refusing to remove the last owner.
//
// An organization with no owner cannot be administered, cannot be deleted, and
// cannot have a new owner appointed -- it is stranded, and recovering it is a
// support ticket against the database. Refusing is cheaper than the recovery
// procedure.
func RemoveMember(ctx context.Context, store OrganizationStore, orgID, accountID string) error {
	m, err := store.MembershipOf(ctx, orgID, accountID)
	if err != nil {
		return err
	}
	if m == nil {
		return ErrNotAMember
	}

	if m.Role == RoleOwner {
		owners, err := store.CountOwners(ctx, orgID)
		if err != nil {
			return err
		}
		if owners <= 1 {
			return fmt.Errorf("auth: cannot remove the last owner of %s", orgID)
		}
	}

	return store.RemoveMember(ctx, orgID, accountID)
}
