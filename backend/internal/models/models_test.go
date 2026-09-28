package models

import (
	"encoding/json"
	"os"
	"testing"

	"gorm.io/gorm"
)

// testTx opens the compose DB as the app user and returns a transaction rolled back after the test.
func testTx(t *testing.T) *gorm.DB {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	db, err := Open(url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	tx := db.Begin()
	t.Cleanup(func() { tx.Rollback() })
	return tx
}

// One round trip per table: insert through GORM, read back, compare a field.
func TestRepositoryRoundTrip(t *testing.T) {
	tx := testTx(t)

	role := Role{Name: "Test Role"}
	must(t, tx.Create(&role).Error)
	must(t, tx.Create(&RolePermission{RoleID: role.ID, Permission: "ticket.view_all"}).Error)
	staff := Staff{Name: "Min", Username: "min.roundtrip", RoleID: role.ID, Language: "th", IsActive: true}
	must(t, tx.Create(&staff).Error)
	cat := Category{Name: Names{"en": "Printers", "my": "ပရင်တာ"}, IsActive: true}
	must(t, tx.Create(&cat).Error)
	loc := Location{Building: Names{"en": "Test B"}, Floor: Names{"en": "9"}, Line: Names{"en": "L9"}, IsActive: true}
	must(t, tx.Create(&loc).Error)
	ticket := Ticket{
		Summary: "Screen flickers", CaseDetails: "Screen flickers after lunch", GuestName: "Nan",
		EmployeeID: "E77", Language: "my", LocationID: loc.ID, CategoryID: &cat.ID,
		AccessTokenHash: make([]byte, 32), Status: StatusNew,
	}
	must(t, tx.Create(&ticket).Error)
	must(t, tx.Create(&Comment{TicketID: ticket.ID, AuthorStaffID: &staff.ID, Body: "Checking", IsInternal: true}).Error)
	must(t, tx.Create(&Attachment{TicketID: ticket.ID, FilePath: "x.jpg", MediaType: "image", SizeBytes: 10}).Error)
	must(t, tx.Create(&AuditEntry{ActorType: ActorStaff, ActorStaffID: &staff.ID, Action: "test.action", TicketID: &ticket.ID}).Error)

	checks := []struct {
		name string
		run  func() error
	}{
		{"roles", func() error { var r Role; return tx.First(&r, role.ID).Error }},
		{"role_permissions", func() error { var p RolePermission; return tx.First(&p, "role_id = ?", role.ID).Error }},
		{"staff", func() error { var s Staff; return tx.First(&s, staff.ID).Error }},
		{"categories", func() error {
			var c Category
			if err := tx.First(&c, cat.ID).Error; err != nil {
				return err
			}
			if c.Name["my"] != "ပရင်တာ" {
				t.Errorf("category name[my] = %q", c.Name["my"])
			}
			return nil
		}},
		{"locations", func() error { var l Location; return tx.First(&l, loc.ID).Error }},
		{"tickets", func() error {
			var got Ticket
			if err := tx.First(&got, ticket.ID).Error; err != nil {
				return err
			}
			if got.Status != StatusNew || got.Priority != nil || got.CreatedAt.IsZero() {
				t.Errorf("ticket = %+v", got)
			}
			return nil
		}},
		{"comments", func() error { var c Comment; return tx.First(&c, "ticket_id = ?", ticket.ID).Error }},
		{"attachments", func() error { var a Attachment; return tx.First(&a, "ticket_id = ?", ticket.ID).Error }},
		{"audit_log", func() error { var a AuditEntry; return tx.First(&a, "ticket_id = ?", ticket.ID).Error }},
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) { must(t, c.run()) })
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// FR-G5, NFR-2: tickets and comments must go through explicit response types,
// so the token hash and internal notes can never be serialized by accident.
func TestSensitiveModelsRefuseJSON_FRG5(t *testing.T) {
	for name, v := range map[string]any{"Ticket": Ticket{}, "Comment": Comment{}} {
		if _, err := json.Marshal(v); err == nil {
			t.Errorf("json.Marshal(%s) succeeded, want error", name)
		}
	}
}
