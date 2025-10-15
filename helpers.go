package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"database/sql"

	"github.com/hashicorp/logutils"
	"github.com/mmcdole/gofeed"
	"github.com/mmcdole/gofeed/atom"
	"github.com/mmcdole/gofeed/rss"
	"github.com/nbd-wtf/go-nostr"
)

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return fallback
}

func convertTimeString(itemTime string) *time.Time {
	// find right date format

	postTime, err := time.Parse(time.RFC3339, itemTime)
	if err != nil {
		postTime, err = time.Parse(time.RFC1123Z, itemTime) // try other one
	}
	if err != nil {
		postTime, err = time.Parse(time.RFC1123, itemTime) // try other one
	}
	if err != nil {
		log.Println("[WARN] Can't parse element time")
	}
	return &postTime
}

func checkMaxAge(itemTime *time.Time, maxAgeHours time.Duration) bool {
	maxAge := time.Now().UTC().Add(-maxAgeHours) // make sure everything is UTC!
	return itemTime.UTC().After(maxAge)
}

func dbInit() *sql.DB {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatalf("[FATAL] open db: %v", err)
	}
	log.Printf("[INFO] database opened at %s", dbPath)
	//defer db.Close()

	_, err = db.Exec(sqlInit)
	if err != nil {
		log.Printf("%q: %s\n", err, sqlInit)
	}

	return db
}

func generateKeysForUrl(feedUrl string) *feedStruct {
	feedElem := feedStruct{}
	feedElem.Url = feedUrl

	feedElem.Sec = nostr.GeneratePrivateKey() // generate new key
	feedElem.Pub, _ = nostr.GetPublicKey(feedElem.Sec)

	return &feedElem
}

func parseDurationWithDays(s string) (time.Duration, error) {
	// Check if string ends with 'd' for days
	if len(s) > 1 && s[len(s)-1] == 'd' {
		// Parse the number part
		dayStr := s[:len(s)-1]
		days, err := time.ParseDuration(dayStr + "h")
		if err != nil {
			return 0, err
		}
		// Convert to days (multiply by 24)
		return days * 24, nil
	}
	// Use standard parsing for other formats
	return time.ParseDuration(s)
}

func logger() {
	filter := &logutils.LevelFilter{
		Levels:   []logutils.LogLevel{"DEBUG", "INFO", "WARN", "ERROR", "FATAL"},
		MinLevel: logutils.LogLevel(logLevel),
		Writer:   os.Stderr,
	}
	log.SetOutput(filter)
}

// ParseFeedWithNativeStructures parses a feed and returns both the universal gofeed.Feed
// and the native atom.Feed or rss.Feed structures for comprehensive data access
func ParseFeedWithNativeStructures(feedURL string, ctx context.Context) (*gofeed.Feed, *atom.Feed, *rss.Feed, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequestWithContext(ctx, "GET", feedURL, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	req.Header.Set("User-Agent", "Gofeed/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, nil, gofeed.HTTPError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
		}
	}

	// Read the entire response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, nil, err
	}

	// Parse with universal parser to get translated feed
	universalFeed, err := gofeed.NewParser().ParseString(string(bodyBytes))
	if err != nil {
		return nil, nil, nil, err
	}

	// Detect feed type and parse natively
	var atomFeed *atom.Feed
	var rssFeed *rss.Feed

	feedType := gofeed.DetectFeedType(bytes.NewReader(bodyBytes))
	switch feedType {
	case gofeed.FeedTypeAtom:
		atomParser := &atom.Parser{}
		atomFeed, err = atomParser.Parse(bytes.NewReader(bodyBytes))
		if err != nil {
			log.Printf("[WARN] Failed to parse atom feed natively: %v", err)
		}
	case gofeed.FeedTypeRSS:
		rssParser := &rss.Parser{}
		rssFeed, err = rssParser.Parse(bytes.NewReader(bodyBytes))
		if err != nil {
			log.Printf("[WARN] Failed to parse RSS feed natively: %v", err)
		}
	}

	return universalFeed, atomFeed, rssFeed, nil
}
