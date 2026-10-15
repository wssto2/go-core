package gormstore

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// The models mirror the migration files column for column (schema_test.go holds them equal). Their types are the
// least that lets GORM create the tables on SQLite for tests, which cannot run the MySQL DDL; the enums are strings.

type incidentModel struct {
	ID                     int        `gorm:"column:id;primaryKey;autoIncrement"`
	PublicCode             string     `gorm:"column:public_code;size:32;not null;uniqueIndex:uq_incidents_public_code"`
	Fingerprint            string     `gorm:"column:fingerprint;type:char(64);not null"`
	OpenFingerprint        *string    `gorm:"column:open_fingerprint;type:char(64);uniqueIndex:uq_incidents_open_fingerprint"`
	Source                 string     `gorm:"column:source;size:20;not null;default:runtime_error"`
	Status                 string     `gorm:"column:status;size:11;not null;default:new;index:idx_incidents_status"`
	Severity               string     `gorm:"column:severity;size:8;not null;default:medium;index:idx_incidents_severity"`
	Visibility             string     `gorm:"column:visibility;size:18;not null;default:reporter_and_staff"`
	Title                  string     `gorm:"column:title;size:255;not null"`
	Description            *string    `gorm:"column:description;type:mediumtext"`
	AffectedModule         *string    `gorm:"column:affected_module;size:100;index:idx_incidents_module"`
	AffectedRoute          *string    `gorm:"column:affected_route;size:255"`
	Environment            *string    `gorm:"column:environment;size:50"`
	ErrorClass             *string    `gorm:"column:error_class;size:255"`
	ErrorCode              *string    `gorm:"column:error_code;size:100"`
	ErrorMessageSample     *string    `gorm:"column:error_message_sample;type:text"`
	StacktraceSample       *string    `gorm:"column:stacktrace_sample;type:mediumtext"`
	FirstSeenAt            time.Time  `gorm:"column:first_seen_at;type:datetime;not null;autoCreateTime:false"`
	LastSeenAt             time.Time  `gorm:"column:last_seen_at;type:datetime;not null;autoCreateTime:false;index:idx_incidents_last_seen_at"`
	OccurrenceCount        int        `gorm:"column:occurrence_count;not null;default:0"`
	ReportCount            int        `gorm:"column:report_count;not null;default:0"`
	CommentCount           int        `gorm:"column:comment_count;not null;default:0"`
	ReporterUserID         *int       `gorm:"column:reporter_user_id;index:idx_incidents_reporter_user_id"`
	AssignedToUserID       *int       `gorm:"column:assigned_to_user_id;index:idx_incidents_assigned_to_user_id"`
	ExternalTicketProvider string     `gorm:"column:external_ticket_provider;size:6;not null;default:none"`
	ExternalTicketID       *string    `gorm:"column:external_ticket_id;size:100"`
	ExternalTicketURL      *string    `gorm:"column:external_ticket_url;size:500"`
	DuplicateOfIncidentID  *int       `gorm:"column:duplicate_of_incident_id;index:idx_incidents_duplicate_of_incident_id"`
	ResolutionSummary      *string    `gorm:"column:resolution_summary;type:text"`
	ResolvedAt             *time.Time `gorm:"column:resolved_at;type:datetime"`
	CreatedAt              time.Time  `gorm:"column:created_at;type:datetime;not null;autoCreateTime:false"`
	UpdatedAt              time.Time  `gorm:"column:updated_at;type:datetime;not null;autoUpdateTime:false"`
}

func (incidentModel) TableName() string { return "incidents" }

type occurrenceModel struct {
	ID                int       `gorm:"column:id;primaryKey;autoIncrement"`
	IncidentID        int       `gorm:"column:incident_id;not null;index:idx_incident_occurrences_incident_id"`
	Fingerprint       string    `gorm:"column:fingerprint;type:char(64);not null;index:idx_incident_occurrences_fingerprint"`
	Environment       string    `gorm:"column:environment;size:50;not null"`
	AffectedModule    *string   `gorm:"column:affected_module;size:100;index:idx_incident_occurrences_module"`
	AffectedRoute     *string   `gorm:"column:affected_route;size:255"`
	RequestMethod     *string   `gorm:"column:request_method;size:10"`
	UserID            *int      `gorm:"column:user_id;index:idx_incident_occurrences_user_id"`
	TenantID          *int      `gorm:"column:tenant_id"`
	IPAddress         *string   `gorm:"column:ip_address;size:45"`
	RequestID         *string   `gorm:"column:request_id;size:100"`
	ErrorClass        *string   `gorm:"column:error_class;size:255"`
	ErrorCode         *string   `gorm:"column:error_code;size:100"`
	Message           string    `gorm:"column:message;type:text;not null"`
	NormalizedMessage *string   `gorm:"column:normalized_message;type:text"`
	Stacktrace        *string   `gorm:"column:stacktrace;type:mediumtext"`
	ContextJSON       *string   `gorm:"column:context_json;type:json"`
	RequestJSON       *string   `gorm:"column:request_json;type:json"`
	OccurredAt        time.Time `gorm:"column:occurred_at;type:datetime;not null;autoCreateTime:false;index:idx_incident_occurrences_occurred_at"`
	CreatedAt         time.Time `gorm:"column:created_at;type:datetime;not null;autoCreateTime:false"`
}

func (occurrenceModel) TableName() string { return "incident_occurrences" }

type reportModel struct {
	ID                   int       `gorm:"column:id;primaryKey;autoIncrement"`
	IncidentID           *int      `gorm:"column:incident_id;index:idx_incident_reports_incident_id"`
	ReportedByUserID     int       `gorm:"column:reported_by_user_id;not null;index:idx_incident_reports_reported_by_user_id"`
	ReportType           string    `gorm:"column:report_type;size:13;not null;default:manual"`
	Status               string    `gorm:"column:status;size:9;not null;default:submitted;index:idx_incident_reports_status"`
	Title                string    `gorm:"column:title;size:255;not null"`
	Description          string    `gorm:"column:description;type:mediumtext;not null"`
	StepsToReproduce     *string   `gorm:"column:steps_to_reproduce;type:mediumtext"`
	ExpectedResult       *string   `gorm:"column:expected_result;type:mediumtext"`
	ActualResult         *string   `gorm:"column:actual_result;type:mediumtext"`
	Severity             string    `gorm:"column:severity;size:8;not null;default:medium"`
	AffectedModule       *string   `gorm:"column:affected_module;size:100"`
	PageURL              *string   `gorm:"column:page_url;size:1000"`
	BrowserInfo          *string   `gorm:"column:browser_info;size:500"`
	AppVersion           *string   `gorm:"column:app_version;size:100"`
	FrontendErrorMessage *string   `gorm:"column:frontend_error_message;type:text"`
	FrontendStacktrace   *string   `gorm:"column:frontend_stacktrace;type:mediumtext"`
	AttachmentJSON       *string   `gorm:"column:attachment_json;type:json"`
	ExtraJSON            *string   `gorm:"column:extra_json;type:json"`
	CreatedAt            time.Time `gorm:"column:created_at;type:datetime;not null;autoCreateTime:false;index:idx_incident_reports_created_at"`
	UpdatedAt            time.Time `gorm:"column:updated_at;type:datetime;not null;autoUpdateTime:false"`
}

func (reportModel) TableName() string { return "incident_reports" }

type commentModel struct {
	ID           int        `gorm:"column:id;primaryKey;autoIncrement"`
	IncidentID   int        `gorm:"column:incident_id;not null;index:idx_incident_comments_incident_id"`
	AuthorUserID int        `gorm:"column:author_user_id;not null;index:idx_incident_comments_author_user_id"`
	Visibility   string     `gorm:"column:visibility;size:8;not null;default:public;index:idx_incident_comments_visibility"`
	Body         string     `gorm:"column:body;type:mediumtext;not null"`
	EditedAt     *time.Time `gorm:"column:edited_at;type:datetime"`
	DeletedAt    *time.Time `gorm:"column:deleted_at;type:datetime"`
	CreatedAt    time.Time  `gorm:"column:created_at;type:datetime;not null;autoCreateTime:false;index:idx_incident_comments_created_at"`
	UpdatedAt    time.Time  `gorm:"column:updated_at;type:datetime;not null;autoUpdateTime:false"`
}

func (commentModel) TableName() string { return "incident_comments" }

type watcherModel struct {
	IncidentID int       `gorm:"column:incident_id;primaryKey;autoIncrement:false"`
	UserID     int       `gorm:"column:user_id;primaryKey;autoIncrement:false;index:idx_incident_watchers_user_id"`
	CreatedAt  time.Time `gorm:"column:created_at;type:datetime;not null;autoCreateTime:false"`
}

func (watcherModel) TableName() string { return "incident_watchers" }

type cooldownModel struct {
	SeverityTier string    `gorm:"column:severity_tier;size:20;primaryKey"`
	LastSentAt   time.Time `gorm:"column:last_sent_at;type:datetime;not null"`
}

func (cooldownModel) TableName() string { return "incident_alert_cooldowns" }

type digestModel struct {
	ID         int        `gorm:"column:id;primaryKey;autoIncrement"`
	IncidentID int        `gorm:"column:incident_id;not null;index:idx_incident_digest_queue_incident_id"`
	PublicCode string     `gorm:"column:public_code;size:32;not null"`
	Title      string     `gorm:"column:title;size:255;not null"`
	Severity   string     `gorm:"column:severity;size:20;not null"`
	CreatedAt  time.Time  `gorm:"column:created_at;type:datetime;not null;autoCreateTime:false"`
	DigestedAt *time.Time `gorm:"column:digested_at;type:datetime;index:idx_incident_digest_queue_digested_at"`
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
