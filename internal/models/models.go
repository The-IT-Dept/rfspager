package models

import (
	"database/sql"
	"time"
)

type System struct {
	ID            int64           `db:"id"`
	SystemID      string          `db:"system_id"` // UUID used in route binding
	UserID        int64           `db:"user_id"`
	Name          string          `db:"name"`
	Lat           sql.NullFloat64 `db:"lat"`
	Lng           sql.NullFloat64 `db:"lng"`
	Sync          bool            `db:"sync"`
	Frequency     int             `db:"frequency"`
	BaseURL       sql.NullString  `db:"base_url"`
	StealCapcodes string          `db:"steal_capcodes"`
	APIKey        sql.NullString  `db:"api_key"` // pagermon-compat ingress key
	LastSyncAt    sql.NullTime    `db:"last_sync_at"`
	CreatedAt     time.Time       `db:"created_at"`
	UpdatedAt     time.Time       `db:"updated_at"`
	DeletedAt     sql.NullTime    `db:"deleted_at"`
}

type Message struct {
	ID        int64          `db:"id"`
	SystemID  int64          `db:"system_id"`
	CapcodeID sql.NullInt64  `db:"capcode_id"`
	Address   string         `db:"address"`
	Message   string         `db:"message"`
	MD5       string         `db:"md5"`
	Lat       sql.NullString `db:"lat"`
	Lng       sql.NullString `db:"lng"`
	CreatedAt time.Time      `db:"created_at"`
	UpdatedAt time.Time      `db:"updated_at"`
	DeletedAt sql.NullTime   `db:"deleted_at"`
}

type Capcode struct {
	ID        int64          `db:"id"`
	Address   string         `db:"address"`
	Alias     string         `db:"alias"`
	Agency    sql.NullString `db:"agency"` // legacy text col kept in sync by trigger; safe to ignore
	AgencyID  sql.NullInt64  `db:"agency_id"`
	Icon      string         `db:"icon"`
	Colour    string         `db:"colour"`
	Ignore    bool           `db:"ignore"`
	CreatedAt time.Time      `db:"created_at"`
	UpdatedAt time.Time      `db:"updated_at"`
	DeletedAt sql.NullTime   `db:"deleted_at"`
}

// CapcodeWithAgency is a Capcode joined with agency name for display purposes.
type CapcodeWithAgency struct {
	Capcode
	AgencyName sql.NullString `db:"agency_name"`
}

type Subscription struct {
	ID        int64     `db:"id"`
	UserID    int64     `db:"user_id"`
	CapcodeID int64     `db:"capcode_id"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

type Installation struct {
	ID        string         `db:"id"` // UUID
	UserID    int64          `db:"user_id"`
	PushType  string         `db:"push_type"`  // "APN" or "GCM"
	PushToken sql.NullString `db:"push_token"`
	ExpoToken sql.NullString `db:"expo_token"`
	Vendor    string         `db:"vendor"`
	Model     string         `db:"model"`
	Version   string         `db:"version"`
	AppID     string         `db:"app_id"`
	AppName   string         `db:"app_name"`
	AppVersion string        `db:"app_version"`
	Timezone  string         `db:"timezone"`
	CreatedAt time.Time      `db:"created_at"`
	UpdatedAt time.Time      `db:"updated_at"`
	DeletedAt sql.NullTime   `db:"deleted_at"`
}

type Alertzone struct {
	ID     int64   `db:"id"`
	UserID int64   `db:"user_id"`
	Name   string  `db:"name"`
	Lat    float64 `db:"lat"`
	Lng    float64 `db:"lng"`
	Radius int     `db:"radius"` // km
}

type User struct {
	ID                int64  `db:"id"`
	SubscriptionValid bool   `db:"subscription_valid"`
}

type Agency struct {
	ID     int64  `db:"id"`
	Agency string `db:"agency"`
}

// NotificationTarget holds the info needed to send a push notification to a user.
type NotificationTarget struct {
	UserID    int64  `db:"user_id"`
	PushType  string `db:"push_type"` // "APN" or "GCM"
	PushToken string `db:"push_token"`
}

// MessageIngress is the payload from external systems (v1 format).
type MessageIngress struct {
	Address   string  `json:"address"`
	Message   string  `json:"message"`
	Timestamp any     `json:"timestamp"` // can be int64 or string
	Agency    string  `json:"agency,omitempty"`
	Alias     string  `json:"alias,omitempty"`
	Icon      string  `json:"icon,omitempty"`
	Color     string  `json:"color,omitempty"`
	Ignore    *bool   `json:"ignore,omitempty"`
}

// MultimonIngress is the v2 multimon_message format.
type MultimonIngress struct {
	Address   string `json:"address"`
	Alpha     string `json:"alpha"`
	Timestamp string `json:"timestamp"`
}
