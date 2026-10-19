package notification

import "time"

// The GORM models of the delivery and settings tables: what Migrate creates them from on SQLite, and the
// shape the stores read and write. The migration files are the tables in production; migrations_test holds
// the two equal.

// deliveryRow is a notification_deliveries row: one send of one notification.
type deliveryRow struct {
	ID             uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	NotificationID uint64     `gorm:"column:notification_id;not null;type:bigint unsigned;uniqueIndex:notification_deliveries_target,priority:1"`
	DeviceID       uint64     `gorm:"column:device_id;not null;type:bigint unsigned;uniqueIndex:notification_deliveries_target,priority:2;index:notification_deliveries_device"`
	Address        *string    `gorm:"column:address;size:255"`
	Channel        string     `gorm:"column:channel;size:16;not null;uniqueIndex:notification_deliveries_target,priority:3"`
	Status         string     `gorm:"column:status;size:16;not null;index:notification_deliveries_due,priority:1;index:notification_deliveries_finished,priority:1"`
	Attempts       uint32     `gorm:"column:attempts;not null;type:int unsigned;default:0"`
	NextAttemptAt  time.Time  `gorm:"column:next_attempt_at;type:datetime;not null;index:notification_deliveries_due,priority:2"`
	ExpiresAt      time.Time  `gorm:"column:expires_at;type:datetime;not null"`
	SentAt         *time.Time `gorm:"column:sent_at;type:datetime"`
	LastStatus     *int16     `gorm:"column:last_status;type:smallint"`
	LastError      *string    `gorm:"column:last_error;size:500"`
	CreatedAt      time.Time  `gorm:"column:created_at;type:datetime;not null"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;type:datetime;not null;index:notification_deliveries_finished,priority:2"`

	// Not stored: the notification's category and, for a failed delivery, why, for the metrics.
	category   string
	failReason string
}

func (deliveryRow) TableName() string { return "notification_deliveries" }

// preferenceRow is a notification_preferences row: a person's own choice for one category and channel.
type preferenceRow struct {
	UserID    uint32    `gorm:"column:user_id;primaryKey;autoIncrement:false;type:int unsigned"`
	Category  string    `gorm:"column:category;primaryKey;size:64"`
	Channel   string    `gorm:"column:channel;primaryKey;size:16"`
	Enabled   bool      `gorm:"column:enabled;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:datetime;not null"`
}

func (preferenceRow) TableName() string { return "notification_preferences" }

// quietRow is a notification_quiet_hours row: a person's own quiet hours.
type quietRow struct {
	UserID      uint32    `gorm:"column:user_id;primaryKey;autoIncrement:false;type:int unsigned"`
	Enabled     bool      `gorm:"column:enabled;not null"`
	StartMinute uint16    `gorm:"column:start_minute;not null;type:smallint unsigned"`
	EndMinute   uint16    `gorm:"column:end_minute;not null;type:smallint unsigned"`
	UpdatedAt   time.Time `gorm:"column:updated_at;type:datetime;not null"`
}

func (quietRow) TableName() string { return "notification_quiet_hours" }

// deviceRow is a notification_devices row: one browser or app that enabled web push.
type deviceRow struct {
	ID            uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	UserID        uint32     `gorm:"column:user_id;not null;type:int unsigned;index:notification_devices_user"`
	Endpoint      string     `gorm:"column:endpoint;size:1024;not null"`
	EndpointHash  string     `gorm:"column:endpoint_hash;type:char(64);not null;uniqueIndex:notification_devices_endpoint_hash"` //nolint:lll // one struct tag
	P256DH        string     `gorm:"column:p256dh;size:128;not null"`
	Auth          string     `gorm:"column:auth;size:64;not null"`
	Label         string     `gorm:"column:label;size:100;not null"`
	Platform      string     `gorm:"column:platform;size:32;not null;default:''"`
	CreatedAt     time.Time  `gorm:"column:created_at;type:datetime;not null"`
	LastSeenAt    time.Time  `gorm:"column:last_seen_at;type:datetime;not null"`
	LastSuccessAt *time.Time `gorm:"column:last_success_at;type:datetime"`
	FailureCount  uint32     `gorm:"column:failure_count;not null;type:int unsigned;default:0"`
	DisabledAt    *time.Time `gorm:"column:disabled_at;type:datetime"`
	UpdatedAt     time.Time  `gorm:"column:updated_at;type:datetime;not null"`
}

func (deviceRow) TableName() string { return "notification_devices" }
