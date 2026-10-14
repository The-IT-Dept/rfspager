package admin

import (
	"context"
	"testing"
	"time"
)

// Admin user views show the store slot and the comp as stored, plus
// pro_access (effective access); system views show whether the owner is
// comped; billing counts running comps apart from store subscriptions.
func TestAdminViewsShowHostComp(t *testing.T) {
	conn := messageDetailTestDB(t)
	store := NewStore(conn)
	ctx := context.Background()

	until := time.Now().UTC().Add(7 * 24 * time.Hour).Truncate(time.Second)
	comped := insertID(t, conn, `INSERT INTO users (user_id, email, password, host_comp_until, host_comp_since)
		VALUES (gen_random_uuid(), 'comped@example.invalid', '!', $1, NOW()) RETURNING id`, until)
	free := insertID(t, conn, `INSERT INTO users (user_id, email, password, host_comp_until)
		VALUES (gen_random_uuid(), 'free@example.invalid', '!', NOW() - INTERVAL '1 day') RETURNING id`)
	system := insertID(t, conn, `INSERT INTO systems (system_id, user_id, name, approved_at, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, 'Budgewoi', NOW(), NOW(), NOW()) RETURNING id`, comped)

	row, err := store.GetUser(ctx, comped)
	if err != nil {
		t.Fatal(err)
	}
	if row["pro_access"] != true || row["subscription_valid"] != false || row["host_comp_until"] == nil || row["host_comp_since"] == nil {
		t.Fatalf("comped user detail = %v", row)
	}
	if row, err = store.GetUser(ctx, free); err != nil || row["pro_access"] != false {
		t.Fatalf("free user detail = %v, %v", row, err)
	}

	list, err := store.ListUsers(ctx, ListParams{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, u := range list.Data {
		if _, ok := u["host_comp_until"]; !ok {
			t.Fatalf("user list row without host_comp_until: %v", u)
		}
		if u["email"] == "comped@example.invalid" && u["pro_access"] == true {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("comped user not listed with pro_access: %v", list.Data)
	}

	sys, err := store.GetSystem(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := sys["owner_host_comp_until"].(time.Time); !ok || !got.Equal(until) {
		t.Fatalf("system owner_host_comp_until = %#v", sys["owner_host_comp_until"])
	}

	billing, err := store.fetchBillingStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if billing.ActiveHostComp != 1 || billing.ActiveTotal != 0 {
		t.Fatalf("billing = %+v", billing)
	}
}
