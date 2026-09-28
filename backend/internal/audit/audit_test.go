package audit

import (
	"os"
	"strings"
	"testing"

	"gorm.io/gorm"

	"ticket-app/internal/models"
)

// FR-L1: the audit row is written in the same transaction as the change,
// so a failed audit write rolls the change back.
func TestRecordFailureRollsBackChange_FRL1(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	db, err := models.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	const name = "FRL1 rollback probe"
	t.Cleanup(func() { db.Where("name->>'en' = ?", name).Delete(&models.Category{}) })

	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&models.Category{Name: models.Names{"en": name}, IsActive: true}).Error; err != nil {
			return err
		}
		// Staff actor without a staff ID violates the audit_log CHECK, so the write fails.
		return Record(tx, Staff(0), "category.created", Change{})
	})
	if err == nil {
		t.Fatal("transaction succeeded, want audit failure")
	}
	var n int64
	db.Model(&models.Category{}).Where("name->>'en' = ?", name).Count(&n)
	if n != 0 {
		t.Fatalf("category rows = %d after failed audit, want 0", n)
	}
}

func TestRecordWritesEntry_FRL1(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	db, err := models.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	defer tx.Rollback()

	ticketID := int64(424242)
	err = Record(tx, System, "ticket.auto_closed", Change{TicketID: &ticketID, From: "resolved", To: "closed", IP: "10.0.0.5"})
	if err != nil {
		t.Fatal(err)
	}
	var got models.AuditEntry
	if err := tx.Where("ticket_id = ?", ticketID).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.ActorType != models.ActorSystem || got.FromValue == nil || *got.FromValue != "resolved" || *got.ToValue != "closed" {
		t.Errorf("entry = %+v", got)
	}
}

// FR-L1: Record refuses a non-transaction handle and a staff actor without an ID, before touching the DB.
func TestRecordGuards_FRL1(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	db, err := models.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	if err := Record(db, System, "test.no_tx", Change{}); err == nil {
		t.Error("Record(db) outside a transaction succeeded, want error")
	}
	tx := db.Begin()
	defer tx.Rollback()
	if err := Record(tx, Staff(0), "test.no_staff", Change{}); err == nil || !strings.Contains(err.Error(), "staff actor without ID") {
		t.Errorf("Record(Staff(0)) err = %v, want staff actor without ID", err)
	}
}
