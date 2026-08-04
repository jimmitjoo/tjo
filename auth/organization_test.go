package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type orgStore struct {
	mu          sync.Mutex
	members     map[string]*Membership // orgID + "/" + accountID
	invitations map[string]*Invitation // hex hash
	accepted    map[string]bool
}

func newOrgStore() *orgStore {
	return &orgStore{
		members:     map[string]*Membership{},
		invitations: map[string]*Invitation{},
		accepted:    map[string]bool{},
	}
}

func mk(orgID, accountID string) string { return orgID + "/" + accountID }

func (s *orgStore) MembershipOf(_ context.Context, orgID, accountID string) (*Membership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.members[mk(orgID, accountID)]
	if !ok {
		return nil, nil
	}
	return m, nil
}

func (s *orgStore) MembershipsFor(_ context.Context, accountID string) ([]*Membership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Membership
	for _, m := range s.members {
		if m.AccountID == accountID {
			out = append(out, m)
		}
	}
	return out, nil
}

func (s *orgStore) AddMember(_ context.Context, m *Membership) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.members[mk(m.OrganizationID, m.AccountID)] = m
	return nil
}

func (s *orgStore) RemoveMember(_ context.Context, orgID, accountID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.members, mk(orgID, accountID))
	return nil
}

func (s *orgStore) SaveInvitation(_ context.Context, inv *Invitation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invitations[key(inv.Hash)] = &Invitation{
		Hash: inv.Hash, OrganizationID: inv.OrganizationID,
		Email: inv.Email, Role: inv.Role, InvitedBy: inv.InvitedBy, Expiry: inv.Expiry,
	}
	return nil
}

func (s *orgStore) ConsumeInvitation(_ context.Context, hash []byte) (*Invitation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(hash)
	inv, ok := s.invitations[k]
	if !ok || s.accepted[k] {
		return nil, ErrInvitationInvalid
	}
	s.accepted[k] = true
	return inv, nil
}

func (s *orgStore) CountOwners(_ context.Context, orgID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.members {
		if m.OrganizationID == orgID && m.Role == RoleOwner {
			n++
		}
	}
	return n, nil
}

func TestAuthorize(t *testing.T) {
	store := newOrgStore()
	perms := DefaultPermissions()
	ctx := context.Background()

	store.AddMember(ctx, &Membership{OrganizationID: "org-1", AccountID: "owner", Role: RoleOwner})
	store.AddMember(ctx, &Membership{OrganizationID: "org-1", AccountID: "member", Role: RoleMember})

	cases := []struct {
		account    string
		permission Permission
		want       error
	}{
		{"owner", PermDeleteOrg, nil},
		{"owner", PermRead, nil},
		{"member", PermRead, nil},
		{"member", PermWrite, nil},
		{"member", PermManageMembers, ErrForbidden},
		{"member", PermDeleteOrg, ErrForbidden},
		{"stranger", PermRead, ErrNotAMember},
	}

	for _, c := range cases {
		err := Authorize(ctx, store, perms, "org-1", c.account, c.permission)
		if !errors.Is(err, c.want) {
			t.Errorf("Authorize(%s, %s) = %v, want %v", c.account, c.permission, err, c.want)
		}
	}
}

// A member of one organization must not be authorized in another. This is the
// whole tenancy boundary, expressed as a membership lookup.
func TestAuthorizeIsScopedToTheOrganization(t *testing.T) {
	store := newOrgStore()
	ctx := context.Background()

	store.AddMember(ctx, &Membership{OrganizationID: "org-a", AccountID: "u1", Role: RoleOwner})

	if err := Authorize(ctx, store, DefaultPermissions(), "org-b", "u1", PermRead); !errors.Is(err, ErrNotAMember) {
		t.Errorf("an owner of org-a was authorized in org-b: %v", err)
	}
}

// Revocation has to be immediate. If the role were cached in the session,
// removing someone would only take effect when they next logged out.
func TestAuthorizeReflectsRemovalImmediately(t *testing.T) {
	store := newOrgStore()
	ctx := context.Background()

	store.AddMember(ctx, &Membership{OrganizationID: "org-1", AccountID: "u1", Role: RoleAdmin})
	if err := Authorize(ctx, store, DefaultPermissions(), "org-1", "u1", PermManageMembers); err != nil {
		t.Fatal(err)
	}

	store.RemoveMember(ctx, "org-1", "u1")

	if err := Authorize(ctx, store, DefaultPermissions(), "org-1", "u1", PermManageMembers); !errors.Is(err, ErrNotAMember) {
		t.Errorf("a removed member was still authorized: %v", err)
	}
}

func TestAcceptInvitation(t *testing.T) {
	ctx := context.Background()

	t.Run("creates the membership with the invited role", func(t *testing.T) {
		store := newOrgStore()
		inv, err := NewInvitation("org-1", "New@Example.com", RoleAdmin, "owner", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		store.SaveInvitation(ctx, inv)

		m, err := AcceptInvitation(ctx, store, inv.PlainText, "u-new", "new@example.com")
		if err != nil {
			t.Fatalf("AcceptInvitation: %v", err)
		}
		if m.Role != RoleAdmin || m.OrganizationID != "org-1" {
			t.Errorf("membership = %+v", m)
		}
	})

	// An invitation is not a bearer token for membership. Without the email
	// check, anyone who obtains the link -- a forwarded email, a shared inbox,
	// a leaked notification -- joins the organization.
	t.Run("rejects an account whose address was not invited", func(t *testing.T) {
		store := newOrgStore()
		inv, _ := NewInvitation("org-1", "invited@example.com", RoleMember, "owner", time.Hour)
		store.SaveInvitation(ctx, inv)

		if _, err := AcceptInvitation(ctx, store, inv.PlainText, "u-other", "someone.else@example.com"); !errors.Is(err, ErrInvitationInvalid) {
			t.Errorf("an uninvited address accepted the invitation: %v", err)
		}
	})

	t.Run("is single use", func(t *testing.T) {
		store := newOrgStore()
		inv, _ := NewInvitation("org-1", "a@example.com", RoleMember, "owner", time.Hour)
		store.SaveInvitation(ctx, inv)

		if _, err := AcceptInvitation(ctx, store, inv.PlainText, "u1", "a@example.com"); err != nil {
			t.Fatal(err)
		}
		if _, err := AcceptInvitation(ctx, store, inv.PlainText, "u2", "a@example.com"); !errors.Is(err, ErrInvitationInvalid) {
			t.Errorf("an accepted invitation was reused: %v", err)
		}
	})

	t.Run("expired invitations are refused", func(t *testing.T) {
		store := newOrgStore()
		inv, _ := NewInvitation("org-1", "a@example.com", RoleMember, "owner", -time.Minute)
		store.SaveInvitation(ctx, inv)

		if _, err := AcceptInvitation(ctx, store, inv.PlainText, "u1", "a@example.com"); !errors.Is(err, ErrInvitationInvalid) {
			t.Errorf("an expired invitation was accepted: %v", err)
		}
	})

	// The invitation token must not be recoverable from storage.
	t.Run("the plaintext is not stored", func(t *testing.T) {
		store := newOrgStore()
		inv, _ := NewInvitation("org-1", "a@example.com", RoleMember, "owner", time.Hour)
		store.SaveInvitation(ctx, inv)

		for _, saved := range store.invitations {
			if saved.PlainText != "" {
				t.Error("the invitation plaintext was stored")
			}
		}
	})
}

// An organization with no owner cannot be administered, deleted, or given a new
// owner. Recovering one is a support ticket against the database.
func TestCannotRemoveTheLastOwner(t *testing.T) {
	store := newOrgStore()
	ctx := context.Background()

	store.AddMember(ctx, &Membership{OrganizationID: "org-1", AccountID: "owner", Role: RoleOwner})
	store.AddMember(ctx, &Membership{OrganizationID: "org-1", AccountID: "member", Role: RoleMember})

	if err := RemoveMember(ctx, store, "org-1", "owner"); err == nil {
		t.Error("the last owner was removed, stranding the organization")
	}

	// A member can go.
	if err := RemoveMember(ctx, store, "org-1", "member"); err != nil {
		t.Errorf("removing a member failed: %v", err)
	}

	// And with a second owner, the first can go too.
	store.AddMember(ctx, &Membership{OrganizationID: "org-1", AccountID: "owner2", Role: RoleOwner})
	if err := RemoveMember(ctx, store, "org-1", "owner"); err != nil {
		t.Errorf("removing an owner with a co-owner failed: %v", err)
	}
}

func TestPermissionsAreExtensible(t *testing.T) {
	perms := DefaultPermissions()
	const RoleBilling Role = "billing"
	perms[RoleBilling] = []Permission{PermManageBilling, PermRead}

	if !perms.Allows(RoleBilling, PermManageBilling) {
		t.Error("a caller-defined role was not granted its permission")
	}
	if perms.Allows(RoleBilling, PermDeleteOrg) {
		t.Error("a caller-defined role was granted a permission it does not have")
	}

	// Extending must not mutate the defaults for the next caller.
	if DefaultPermissions()[RoleBilling] != nil {
		t.Error("DefaultPermissions returns shared state")
	}
}
