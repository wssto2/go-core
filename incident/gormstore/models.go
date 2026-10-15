package gormstore

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// The models mirror the migration files column for column (schema_test.go holds them equal). Their types are the
// least that lets GORM create the tables on SQLite for tests, which cannot run the MySQL DDL; the enums are strings.

type incidentModel struct {
	ID                     int        `gorm:"primaryKey;autoIncrement"`
	PublicCode             string     `gorm:"size:32;not null;uniqueIndex:uq_incidents_public_code"`
	Fingerprint            string     `gorm:"type:char(64);not null"`
	OpenFingerprint        *string    `gorm:"type:char(64);uniqueIndex:uq_incidents_open_fingerprint"`
	Source                 string     `gorm:"size:20;not null;default:runtime_error"`
	Status                 string     `gorm:"size:11;not null;default:new;index:idx_incidents_status"`
	Severity               string     `gorm:"size:8;not null;default:medium;index:idx_incidents_severity"`
	Visibility             string     `gorm:"size:18;not null;default:reporter_and_staff"`
	Title                  string     `gorm:"size:255;not null"`
	Description            *string    `gorm:"type:mediumtext"`
	AffectedModule         *string    `gorm:"size:100;index:idx_incidents_module"`
	AffectedRoute          *string    `gorm:"size:255"`
	Environment            *string    `gorm:"size:50"`
	ErrorClass             *string    `gorm:"size:255"`
	ErrorCode              *string    `gorm:"size:100"`
	ErrorMessageSample     *string    `gorm:"type:text"`
	StacktraceSample       *string    `gorm:"type:mediumtext"`
	FirstSeenAt            time.Time  `gorm:"type:datetime;not null"`
	LastSeenAt             time.Time  `gorm:"type:datetime;not null;index:idx_incidents_last_seen_at"`
	OccurrenceCount        int        `gorm:"not null;default:0"`
	ReportCount            int        `gorm:"not null;default:0"`
	CommentCount           int        `gorm:"not null;default:0"`
	ReporterUserID         *int       `gorm:"index:idx_incidents_reporter_user_id"`
	AssignedToUserID       *int       `gorm:"index:idx_incidents_assigned_to_user_id"`
	ExternalTicketProvider string     `gorm:"size:6;not null;default:none"`
	ExternalTicketID       *string    `gorm:"size:100"`
	ExternalTicketURL      *string    `gorm:"size:500"`
	DuplicateOfIncidentID  *int       `gorm:"index:idx_incidents_duplicate_of_incident_id"`
	ResolutionSummary      *string    `gorm:"type:text"`
	ResolvedAt             *time.Time `gorm:"type:datetime"`
	CreatedAt              time.Time  `gorm:"type:datetime;not null"`
	UpdatedAt              time.Time  `gorm:"type:datetime;not null;autoUpdateTime:false"`
}

func (incidentModel) TableName() string { return "incidents" }

type occurrenceModel struct {
	ID                int     `gorm:"primaryKey;autoIncrement"`
	IncidentID        int     `gorm:"not null;index:idx_incident_occurrences_incident_id"`
	Fingerprint       string  `gorm:"type:char(64);not null;index:idx_incident_occurrences_fingerprint"`
	Environment       string  `gorm:"size:50;not null"`
	AffectedModule    *string `gorm:"size:100;index:idx_incident_occurrences_module"`
	AffectedRoute     *string `gorm:"size:255"`
	RequestMethod     *string `gorm:"size:10"`
	UserID            *int    `gorm:"index:idx_incident_occurrences_user_id"`
	TenantID          *int
	IPAddress         *string   `gorm:"size:45"`
	RequestID         *string   `gorm:"size:100"`
	ErrorClass        *string   `gorm:"size:255"`
	ErrorCode         *string   `gorm:"size:100"`
	Message           string    `gorm:"type:text;not null"`
	NormalizedMessage *string   `gorm:"type:text"`
	Stacktrace        *string   `gorm:"type:mediumtext"`
	ContextJSON       *string   `gorm:"type:json"`
	RequestJSON       *string   `gorm:"type:json"`
	OccurredAt        time.Time `gorm:"type:datetime;not null;index:idx_incident_occurrences_occurred_at"`
	CreatedAt         time.Time `gorm:"type:datetime;not null"`
}

func (occurrenceModel) TableName() string { return "incident_occurrences" }

type reportModel struct {
	ID                   int       `gorm:"primaryKey;autoIncrement"`
	IncidentID           *int      `gorm:"index:idx_incident_reports_incident_id"`
	ReportedByUserID     int       `gorm:"not null;index:idx_incident_reports_reported_by_user_id"`
	ReportType           string    `gorm:"size:13;not null;default:manual"`
	Status               string    `gorm:"size:9;not null;default:submitted;index:idx_incident_reports_status"`
	Title                string    `gorm:"size:255;not null"`
	Description          string    `gorm:"type:mediumtext;not null"`
	StepsToReproduce     *string   `gorm:"type:mediumtext"`
	ExpectedResult       *string   `gorm:"type:mediumtext"`
	ActualResult         *string   `gorm:"type:mediumtext"`
	Severity             string    `gorm:"size:8;not null;default:medium"`
	AffectedModule       *string   `gorm:"size:100"`
	PageURL              *string   `gorm:"size:1000"`
	BrowserInfo          *string   `gorm:"size:500"`
	AppVersion           *string   `gorm:"size:100"`
	FrontendErrorMessage *string   `gorm:"type:text"`
	FrontendStacktrace   *string   `gorm:"type:mediumtext"`
	AttachmentJSON       *string   `gorm:"type:json"`
	ExtraJSON            *string   `gorm:"type:json"`
	CreatedAt            time.Time `gorm:"type:datetime;not null;index:idx_incident_reports_created_at"`
	UpdatedAt            time.Time `gorm:"type:datetime;not null;autoUpdateTime:false"`
}

func (reportModel) TableName() string { return "incident_reports" }

type commentModel struct {
	ID           int        `gorm:"primaryKey;autoIncrement"`
	IncidentID   int        `gorm:"not null;index:idx_incident_comments_incident_id"`
	AuthorUserID int        `gorm:"not null;index:idx_incident_comments_author_user_id"`
	Visibility   string     `gorm:"size:8;not null;default:public;index:idx_incident_comments_visibility"`
	Body         string     `gorm:"type:mediumtext;not null"`
	EditedAt     *time.Time `gorm:"type:datetime"`
	DeletedAt    *time.Time `gorm:"type:datetime"`
	CreatedAt    time.Time  `gorm:"type:datetime;not null;index:idx_incident_comments_created_at"`
	UpdatedAt    time.Time  `gorm:"type:datetime;not null;autoUpdateTime:false"`
}

func (commentModel) TableName() string { return "incident_comments" }

type watcherModel struct {
	IncidentID int       `gorm:"primaryKey;autoIncrement:false"`
	UserID     int       `gorm:"primaryKey;autoIncrement:false;index:idx_incident_watchers_user_id"`
	CreatedAt  time.Time `gorm:"type:datetime;not null"`
}

func (watcherModel) TableName() string { return "incident_watchers" }

type cooldownModel struct {
	SeverityTier string    `gorm:"size:20;primaryKey"`
	LastSentAt   time.Time `gorm:"type:datetime;not null"`
}

func (cooldownModel) TableName() string { return "incident_alert_cooldowns" }

type digestModel struct {
	ID         int        `gorm:"primaryKey;autoIncrement"`
	IncidentID int        `gorm:"not null;index:idx_incident_digest_queue_incident_id"`
	PublicCode string     `gorm:"size:32;not null"`
	Title      string     `gorm:"size:255;not null"`
	Severity   string     `gorm:"size:20;not null"`
	CreatedAt  time.Time  `gorm:"type:datetime;not null"`
	DigestedAt *time.Time `gorm:"type:datetime;index:idx_incident_digest_queue_digested_at"`
}

func (digestModel) TableName() string { return "incident_digest_queue" }

// Migrate creates the tables from the models, for tests on SQLite; production runs the files in migrations.
func Migrate(db *gorm.DB) error {
	err := db.AutoMigrate(new(incidentModel), new(occurrenceModel), new(reportModel), new(commentModel),
		new(watcherModel), new(cooldownModel), new(digestModel))
	if err != nil {
		return fmt.Errorf("incident: migrate: %w", err)
	}

	return nil
}
