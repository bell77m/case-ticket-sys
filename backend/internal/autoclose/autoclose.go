// Package autoclose closes tickets that have been Resolved for 7 days (FR-T5), as actor "system" (FR-L1).
package autoclose

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ticket-app/internal/audit"
	"ticket-app/internal/events"
	"ticket-app/internal/models"
)

const (
	lockKey = "autoclose:lock"
	// lockTTL is never released early, so pods whose hourly tickers fire at other minutes skip the same hour.
	lockTTL = 55 * time.Minute
	// closeAfter is how long a ticket stays Resolved before it closes (FR-T5).
	closeAfter = 7 * 24 * time.Hour
)

var errSkip = errors.New("no longer eligible")

// Run closes every ticket resolved at or before now-closeAfter, one transaction per ticket, and returns how many
// it closed; each closed ticket is published after its commit (FR-P3). It does nothing when another pod holds this hour's lock. now is a parameter so tests can fake it.
func Run(ctx context.Context, db *gorm.DB, rdb *redis.Client, now time.Time) (int, error) {
	ok, err := rdb.SetNX(ctx, lockKey, now.UTC().Format(time.RFC3339), lockTTL).Result()
	if err != nil {
		return 0, fmt.Errorf("autoclose lock: %w", err)
	}
	if !ok {
		return 0, nil
	}
	cutoff := now.Add(-closeAfter)
	db = db.WithContext(ctx)
	var ids []int64
	if err := db.Model(&models.Ticket{}).Where("status = ? AND resolved_at <= ?", models.StatusResolved, cutoff).
		Pluck("id", &ids).Error; err != nil {
		return 0, fmt.Errorf("autoclose select: %w", err)
	}
	closed := 0
	var errs []error
	for _, id := range ids {
		err := db.Transaction(func(tx *gorm.DB) error {
			var t models.Ticket
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&t, id).Error; err != nil {
				return err
			}
			// Re-check under the lock: a guest may have replied (reopened) since the select.
			if t.Status != models.StatusResolved || t.ResolvedAt == nil || t.ResolvedAt.After(cutoff) {
				return errSkip
			}
			if err := tx.Model(&t).Update("status", models.StatusClosed).Error; err != nil {
				return err
			}
			return audit.Record(tx, audit.System, "ticket.auto_closed",
				audit.Change{TicketID: &id, From: models.StatusResolved, To: models.StatusClosed})
		})
		switch {
		case err == nil:
			closed++
			_ = events.Publish(ctx, rdb, id) // FR-P3; logged inside, and the ticket is closed either way
		case errors.Is(err, errSkip), errors.Is(err, gorm.ErrRecordNotFound):
		default:
			slog.Error("auto-close ticket", "ticket_id", id, "error", err)
			errs = append(errs, fmt.Errorf("ticket %d: %w", id, err))
		}
	}
	return closed, errors.Join(errs...)
}
