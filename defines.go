package main

import (
	"database/sql"
	"strconv"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
)

var fetchInterval, _ = time.ParseDuration(getEnv("FETCH_INTERVAL", "15m"))
var metadataInterval, _ = time.ParseDuration(getEnv("METADATA_INTERVAL", "12h"))
var maxPostAge, _ = time.ParseDuration(getEnv("MAX_POST_AGE", "24h"))

// historyInterval removed - no longer using time-based duplicate detection
var logLevel = getEnv("LOG_LEVEL", "DEBUG")
var webserverPort = getEnv("WEBSERVER_PORT", "8061")
var nip05Domain = getEnv("NIP05_DOMAIN", "atomstr.data.haus")
var maxWorkers, _ = strconv.Atoi(getEnv("MAX_WORKERS", "5"))
var r = getEnv("RELAYS_TO_PUBLISH_TO", "wss://nostr.data.haus, wss://nos.lol, wss://relay.damus.io")
var relaysToPublishTo = strings.Split(r, ", ")
var defaultFeedImage = getEnv("DEFAULT_FEED_IMAGE", "https://void.cat/d/NDrSDe4QMx9jh6bD9LJwcK")
var dbPath = getEnv("DB_PATH", "./atomstr.db")
var noPub, _ = strconv.ParseBool(getEnv("NOPUB", "false"))
var atomstrversion string = "0.9.6"

type Atomstr struct {
	db *sql.DB
	// Registered hooks invoked before publishing/signing a Nostr event
	prePublishHooks []NostrEventHook
}

var sqlInit = `
CREATE TABLE IF NOT EXISTS feeds (
	pub VARCHAR(64) PRIMARY KEY,
	sec VARCHAR(64) NOT NULL,
	url TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS published_posts (
	url TEXT PRIMARY KEY,
	feed_url TEXT NOT NULL,
	published_at INTEGER NOT NULL,
	nostr_event_id TEXT NOT NULL,
	FOREIGN KEY (feed_url) REFERENCES feeds(url)
);
CREATE INDEX IF NOT EXISTS idx_published_posts_feed_url ON published_posts(feed_url);
CREATE INDEX IF NOT EXISTS idx_published_posts_published_at ON published_posts(published_at);
CREATE INDEX IF NOT EXISTS idx_published_posts_nostr_event_id ON published_posts(nostr_event_id);
`

type feedStruct struct {
	Url         string         `json:"url"`
	Sec         string         `json:"-"`
	Pub         string         `json:"pub"`
	Npub        string         `json:"npub"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Link        string         `json:"link"`
	Image       string         `json:"image"`
	Posts       []*gofeed.Item `json:"-"`
}

// feedPostStruct is a stable representation of a single feed post for external APIs.
type feedPostStruct struct {
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	Link          string   `json:"link"`
	GUID          string   `json:"guid"`
	Published     string   `json:"published"`
	PublishedUnix int64    `json:"published_unix"`
	Categories    []string `json:"categories"`
	Enclosures    []string `json:"enclosures"`
}

type webIndex struct {
	Relays  []string
	Feeds   []feedStruct
	Version string
}
type webAddFeed struct {
	Status string
	Feed   feedStruct
}
