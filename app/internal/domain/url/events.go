package url

import "time"

type URLCreated struct {
	URLID       string
	UserID      string
	OriginalURL string
	ShortCode   string
	ExpiresOn   time.Time
	OccurredAt  time.Time
}

type URLAccessed struct {
	URLID       string
	ShortCode   string
	IPAddress   string
	UserAgent   string
	Referer     string
	CountryCode string // resolved by GeoIP before publishing
	DeviceType  string // mobile | desktop | bot
	OccurredAt  time.Time
}

type URLExpired struct {
	URLID      string
	ShortCode  string
	OccurredAt time.Time
}

type URLSuspended struct {
	URLID      string
	Reason     string // "malware" | "phishing" | "spam"
	OccurredAt time.Time
}

type URLDeleted struct {
	URLID      string
	ShortCode  string
	OccurredAt time.Time
}
