// Package audit writes the append-only activity log (FR-L1).
// Call Record with the same transaction as the change it describes, so both commit or neither does.
package audit

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"ticket-app/internal/models"
)

// Actor is who performed an action.
type Actor struct {
	Type    string
	StaffID *int64
}

// System is the actor for scheduled jobs such as auto-close.
var System = Actor{Type: models.ActorSystem}

// Guest is the actor for actions taken through a tracking link or the public form.
var Guest = Actor{Type: models.ActorGuest}

// Staff returns the actor for a logged-in staff member.
func Staff(id int64) Actor {
	if id == 0 {
		return Actor{Type: models.ActorStaff} // rejected by Record and by the audit_log CHECK constraint
	}
	return Actor{Type: models.ActorStaff, StaffID: &id}
}

// Change describes what an action touched. Empty strings are stored as NULL.
type Change struct {
	TicketID *int64
	Target   string
	From     string
	To       string
	IP       string
}

// Record inserts one audit_log row inside tx, which must be a transaction (db.Transaction or db.Begin).
func Record(tx *gorm.DB, actor Actor, action string, c Change) error {
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return errors.New("audit: Record needs a transaction, not the connection pool")
	}
	if actor.Type == models.ActorStaff && actor.StaffID == nil {
		return fmt.Errorf("audit %s: staff actor without ID", action)
	}
	e := models.AuditEntry{
		ActorType:    actor.Type,
		ActorStaffID: actor.StaffID,
		Action:       action,
		TicketID:     c.TicketID,
		Target:       nullable(c.Target),
		FromValue:    nullable(c.From),
		ToValue:      nullable(c.To),
		IPAddress:    nullable(c.IP),
	}
	if err := tx.Create(&e).Error; err != nil {
		return fmt.Errorf("audit %s: %w", action, err)
	}
	return nil
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
