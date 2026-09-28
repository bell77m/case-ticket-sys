// Package models maps the tables in backend/migrations to GORM structs.
// The schema is owned by goose migrations; never call AutoMigrate.
package models

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open connects as the app user with a bounded connection pool.
func Open(databaseURL string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{
		// ParameterizedQueries keeps guest names, employee IDs and case details out of logged SQL.
		Logger: logger.New(slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn), logger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			ParameterizedQueries:      true,
		}),
		SkipDefaultTransaction: true, // callers open explicit transactions for audited writes
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("database handle: %w", err)
	}
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}

// Names holds one value per language: en (required), zh-CN, my, th (FR-I4).
type Names map[string]string

// Ticket statuses (stored as codes, translated in the frontend).
const (
	StatusNew        = "new"
	StatusInProgress = "in_progress"
	StatusWaiting    = "waiting"
	StatusResolved   = "resolved"
	StatusClosed     = "closed"
)

// Audit actor types.
const (
	ActorStaff  = "staff"
	ActorGuest  = "guest"
	ActorSystem = "system"
)

type Role struct {
	ID   int64
	Name string
}

type RolePermission struct {
	RoleID     int64  `gorm:"primaryKey"`
	Permission string `gorm:"primaryKey"`
}

type Staff struct {
	ID                 int64
	Name               string
	Username           string // sign-in name, lower case (FR-A1)
	PasswordHash       string // "" until a password is set; see auth.HashPassword (FR-A7)
	MustChangePassword bool   // a temporary password must be replaced first (FR-A8)
	PasswordChangedAt  time.Time
	RoleID             int64
	Language           string
	IsActive           bool
	CreatedAt          time.Time
}

func (Staff) TableName() string { return "staff" }

type Category struct {
	ID       int64
	Name     Names `gorm:"serializer:json;type:jsonb"`
	IsActive bool
}

type Location struct {
	ID       int64
	Building Names `gorm:"serializer:json;type:jsonb"`
	Floor    Names `gorm:"serializer:json;type:jsonb"`
	Line     Names `gorm:"serializer:json;type:jsonb"`
	IsActive bool
}

type Ticket struct {
	ID              int64
	Summary         string
	CaseDetails     string
	CategoryID      *int64
	Priority        *string // NULL until staff triage (FR-T3)
	Status          string
	GuestName       string
	EmployeeID      string
	Language        string
	LocationID      int64
	AccessTokenHash []byte `json:"-"` // SHA-256 of the tracking token; the raw token is never stored (NFR-2)
	AssigneeID      *int64
	FirstResponseAt *time.Time
	ResolvedAt      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// errUseResponseType stops Ticket and Comment being sent to clients directly (FR-G5, NFR-2):
// handlers map them to an explicit guest or staff response type instead.
var errUseResponseType = errors.New("models: map to a response type before encoding")

// MarshalJSON always fails; see errUseResponseType.
func (Ticket) MarshalJSON() ([]byte, error) { return nil, errUseResponseType }

// MarshalJSON always fails; see errUseResponseType.
func (Comment) MarshalJSON() ([]byte, error) { return nil, errUseResponseType }

// MarshalJSON always fails; see errUseResponseType. Staff holds the password hash.
func (Staff) MarshalJSON() ([]byte, error) { return nil, errUseResponseType }

type Comment struct {
	ID            int64
	TicketID      int64
	AuthorStaffID *int64 // nil = guest
	Body          string
	IsInternal    bool
	CreatedAt     time.Time
}

type Attachment struct {
	ID                int64
	TicketID          int64
	FilePath          string
	MediaType         string // image or video
	SizeBytes         int64
	UploadedByStaffID *int64 // nil = guest
	CreatedAt         time.Time
}

// AuditEntry is one row of the append-only audit_log. Write it only through audit.Record.
type AuditEntry struct {
	ID           int64
	ActorType    string
	ActorStaffID *int64
	Action       string
	TicketID     *int64
	Target       *string
	FromValue    *string
	ToValue      *string
	IPAddress    *string `gorm:"type:inet"`
	CreatedAt    time.Time
}

func (AuditEntry) TableName() string { return "audit_log" }
