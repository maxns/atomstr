package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mmcdole/gofeed/atom"
	"github.com/mmcdole/gofeed/rss"
	"github.com/nbd-wtf/go-nostr"
)

// Register this hook in the custom hook registry
func init() {
	RegisterCustomHook("credoIngestArticle", NewCredoIngestArticleHookFromConfig)
}

// retryTracker is a thread-safe in-memory retry count tracker
type retryTracker struct {
	mu     sync.RWMutex
	counts map[string]int
}

func newRetryTracker() *retryTracker {
	return &retryTracker{
		counts: make(map[string]int),
	}
}

func (rt *retryTracker) getRetryCount(url string) int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.counts[url]
}

func (rt *retryTracker) incrementRetryCount(url string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.counts[url]++
}

func (rt *retryTracker) clearRetryCount(url string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	delete(rt.counts, url)
}

// truncateContent truncates content to maxContentSizeK * 1024 bytes and adds [TRUNCATED] suffix
func (h *CredoIngestArticleHook) truncateContent(content string) string {
	maxBytes := h.maxContentSizeK * 1024
	if len(content) <= maxBytes {
		return content
	}
	truncated := content[:maxBytes]
	return truncated + "[TRUNCATED]"
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// CredoIngestArticleHook calls the Credo API to ingest articles for AI analysis
// It extracts article content from Nostr events and sends it to the Credo API
type CredoIngestArticleHook struct {
	url             string
	headers         map[string]string
	client          *http.Client
	retryTracker    *retryTracker
	maxContentSizeK int
	retryLimit      int
}

func NewCredoIngestArticleHook(endpoint string, headers map[string]string, maxContentSizeK int, retryLimit int) *CredoIngestArticleHook {
	return &CredoIngestArticleHook{
		url:             endpoint,
		headers:         headers,
		client:          &http.Client{Timeout: 30 * time.Second}, // Longer timeout for AI processing
		retryTracker:    newRetryTracker(),
		maxContentSizeK: maxContentSizeK,
		retryLimit:      retryLimit,
	}
}

// NewCredoIngestArticleHookFromConfig creates a hook from configuration map
func NewCredoIngestArticleHookFromConfig(config map[string]interface{}) (NostrEventHook, error) {
	// Extract URL
	url, ok := config["url"].(string)
	if !ok || url == "" {
		return nil, errors.New("credoIngestArticle hook requires 'url' field")
	}

	// Extract headers
	headers := make(map[string]string)
	if headersRaw, exists := config["headers"]; exists {
		if headersMap, ok := headersRaw.(map[string]interface{}); ok {
			for k, v := range headersMap {
				if strVal, ok := v.(string); ok {
					headers[k] = strVal
				}
			}
		}
	}

	// Extract maxContentSizeK (default: 16)
	maxContentSizeK := 16
	if maxContentSizeKRaw, exists := config["maxContentSizeK"]; exists {
		switch v := maxContentSizeKRaw.(type) {
		case int:
			maxContentSizeK = v
		case float64:
			maxContentSizeK = int(v)
		case string:
			if parsed, err := strconv.Atoi(v); err == nil {
				maxContentSizeK = parsed
			}
		}
	}

	// Extract retryLimit (default: 3)
	retryLimit := 3
	if retryLimitRaw, exists := config["retryLimit"]; exists {
		switch v := retryLimitRaw.(type) {
		case int:
			retryLimit = v
		case float64:
			retryLimit = int(v)
		case string:
			if parsed, err := strconv.Atoi(v); err == nil {
				retryLimit = parsed
			}
		}
	}

	return NewCredoIngestArticleHook(url, headers, maxContentSizeK, retryLimit), nil
}

// CredoIngestArticleRequest per API specification
type credoIngestArticleRequest struct {
	URL      string `json:"url"`
	Title    string `json:"title,omitempty"`
	Content  string `json:"content"`
	Excerpt  string `json:"excerpt,omitempty"`
	Language string `json:"language,omitempty"`
	GUID     string `json:"GUID,omitempty"`
	// Full atom details as per API specification - raw native data for backend parsing
	AtomMeta *credoAtomMeta `json:"atomMeta,omitempty"`
}

// credoAtomMeta represents the atomMeta structure as per API specification
type credoAtomMeta struct {
	Feed *credoAtomFeed  `json:"feed,omitempty"`
	Item *credoAtomEntry `json:"item,omitempty"`
}

// credoAtomFeed represents an Atom feed as per RFC 4287
type credoAtomFeed struct {
	ID           string               `json:"id,omitempty"`
	Title        *credoAtomText       `json:"title,omitempty"`
	Updated      string               `json:"updated,omitempty"`
	Subtitle     *credoAtomText       `json:"subtitle,omitempty"`
	Links        []*credoAtomLink     `json:"links,omitempty"`
	Language     string               `json:"language,omitempty"`
	Generator    *credoAtomGenerator  `json:"generator,omitempty"`
	Icon         string               `json:"icon,omitempty"`
	Logo         string               `json:"logo,omitempty"`
	Rights       *credoAtomText       `json:"rights,omitempty"`
	Contributors []*credoAtomPerson   `json:"contributors,omitempty"`
	Authors      []*credoAtomPerson   `json:"authors,omitempty"`
	Categories   []*credoAtomCategory `json:"categories,omitempty"`
}

// credoAtomEntry represents an Atom entry as per RFC 4287
type credoAtomEntry struct {
	ID           string               `json:"id,omitempty"`
	Title        *credoAtomText       `json:"title,omitempty"`
	Updated      string               `json:"updated,omitempty"`
	Summary      *credoAtomText       `json:"summary,omitempty"`
	Authors      []*credoAtomPerson   `json:"authors,omitempty"`
	Contributors []*credoAtomPerson   `json:"contributors,omitempty"`
	Categories   []*credoAtomCategory `json:"categories,omitempty"`
	Links        []*credoAtomLink     `json:"links,omitempty"`
	Rights       *credoAtomText       `json:"rights,omitempty"`
	Published    string               `json:"published,omitempty"`
	Source       *credoAtomSource     `json:"source,omitempty"`
	Content      *credoAtomContent    `json:"content,omitempty"`
}

// Supporting types for atom structures
type credoAtomText struct {
	Type  string `json:"type,omitempty"`
	Value string `json:"value,omitempty"`
}

type credoAtomPerson struct {
	Name  string `json:"name,omitempty"`
	URI   string `json:"uri,omitempty"`
	Email string `json:"email,omitempty"`
}

type credoAtomCategory struct {
	Term   string `json:"term,omitempty"`
	Scheme string `json:"scheme,omitempty"`
	Label  string `json:"label,omitempty"`
}

type credoAtomLink struct {
	Href     string `json:"href,omitempty"`
	Rel      string `json:"rel,omitempty"`
	Type     string `json:"type,omitempty"`
	Hreflang string `json:"hreflang,omitempty"`
	Title    string `json:"title,omitempty"`
	Length   string `json:"length,omitempty"`
}

type credoAtomGenerator struct {
	Value   string `json:"value,omitempty"`
	URI     string `json:"uri,omitempty"`
	Version string `json:"version,omitempty"`
}

type credoAtomContent struct {
	Type  string `json:"type,omitempty"`
	Value string `json:"value,omitempty"`
	Src   string `json:"src,omitempty"`
}

type credoAtomSource struct {
	Title        *credoAtomText       `json:"title,omitempty"`
	ID           string               `json:"id,omitempty"`
	Updated      string               `json:"updated,omitempty"`
	Subtitle     *credoAtomText       `json:"subtitle,omitempty"`
	Links        []*credoAtomLink     `json:"links,omitempty"`
	Generator    *credoAtomGenerator  `json:"generator,omitempty"`
	Icon         string               `json:"icon,omitempty"`
	Logo         string               `json:"logo,omitempty"`
	Rights       *credoAtomText       `json:"rights,omitempty"`
	Contributors []*credoAtomPerson   `json:"contributors,omitempty"`
	Authors      []*credoAtomPerson   `json:"authors,omitempty"`
	Categories   []*credoAtomCategory `json:"categories,omitempty"`
}

type credoIngestArticleResponse struct {
	Success   bool     `json:"success"`
	Tags      []string `json:"tags,omitempty"` // Root level tags array from API response
	ArticleId string   `json:"articleId,omitempty"`
	Features  []string `json:"features,omitempty"`
	Assists   *struct {
		GetFeatures *struct {
			Features                      []string `json:"features"`
			EstimatedReadTimeMinutes      float64  `json:"estimatedReadTimeMinutes"`
			TopicHashTags                 []string `json:"topicHashTags"`
			Clarity                       string   `json:"clarity"`
			LanguageCode                  string   `json:"languageCode"`
			Tone                          string   `json:"tone"`
			Length                        string   `json:"length"`
			Complexity                    string   `json:"complexity"`
			AnalysisSummaryInUserLanguage string   `json:"analysisSummaryInUserLanguage"`
		} `json:"getFeatures,omitempty"`
		Unbait *struct {
			ClickbaitScore float64 `json:"clickbaitScore"`
			Feature        string  `json:"feature"`
			Meta           struct {
				ArticleMeta struct {
					Title  string        `json:"title"`
					Links  []interface{} `json:"links"`
					Images []interface{} `json:"images"`
				} `json:"articleMeta"`
			} `json:"meta"`
			UnbaitAnswer string `json:"unbaitAnswer"`
			Why          string `json:"why"`
			UnbaitTitle  string `json:"unbaitTitle"`
		} `json:"unbait,omitempty"`
	} `json:"assists,omitempty"`
	Message string `json:"message,omitempty"`
	TimeMs  int64  `json:"timeMs,omitempty"`
}

func (h *CredoIngestArticleHook) BeforePublish(ctx context.Context, feed feedStruct, feedPost feedPostStruct, event *nostr.Event) (*nostr.Event, error) {
	log.Printf("[DEBUG] credo-ingest-article: processing event %s with content length %d", event.ID, len(event.Content))

	// Check retry count before attempting analysis
	retryCount := h.retryTracker.getRetryCount(feedPost.Link)
	log.Printf("[DEBUG] credo-ingest-article: retry count for url %s is %d (limit: %d)", feedPost.Link, retryCount, h.retryLimit)
	if retryCount >= h.retryLimit {
		log.Printf("[WARN] credo-ingest-article: skipping analysis for url %s - retry count %d exceeds limit %d", feedPost.Link, retryCount, h.retryLimit)
		return event, nil // Return original event without analysis
	}

	// Build request with raw atom feed data for backend parsing
	reqBody := credoIngestArticleRequest{}

	// Truncate URL, Title, GUID to 512 chars max
	reqBody.URL = truncateString(feedPost.Link, 512)
	reqBody.Title = truncateString(feedPost.Title, 512)
	reqBody.GUID = truncateString(feedPost.GUID, 512)
	reqBody.Language = "en" // Default to English (already short)

	// Truncate content if it exceeds maxContentSizeK
	content := feedPost.Content
	if h.maxContentSizeK > 0 {
		originalLength := len(content)
		content = h.truncateContent(content)
		if len(content) < originalLength {
			log.Printf("[INFO] credo-ingest-article: truncated content from %d to %d bytes for url %s", originalLength, len(content), feedPost.Link)
		}
	}
	reqBody.Content = content

	// Truncate excerpt to same limit as content (maxContentSizeK * 1024)
	excerpt := feedPost.Description
	if h.maxContentSizeK > 0 {
		maxExcerptBytes := h.maxContentSizeK * 1024
		if len(excerpt) > maxExcerptBytes {
			originalLength := len(excerpt)
			excerpt = truncateString(excerpt, maxExcerptBytes)
			log.Printf("[INFO] credo-ingest-article: truncated excerpt from %d to %d bytes for url %s", originalLength, len(excerpt), feedPost.Link)
		}
	}
	reqBody.Excerpt = excerpt

	// Build comprehensive atomMeta structure from native feed data
	// Let the backend parse and extract enhanced fields from this raw data
	if feedPost.FeedType == "atom" && feedPost.AtomEntry != nil {
		reqBody.AtomMeta = buildAtomMeta(feedPost.AtomFeed, feedPost.AtomEntry)
	} else if feedPost.FeedType == "rss" && feedPost.RSSItem != nil {
		// Map RSS to atom format for the API
		reqBody.AtomMeta = buildAtomMetaFromRSS(feedPost.RSSFeed, feedPost.RSSItem)
	}

	buf, err := json.Marshal(&reqBody)
	if err != nil {
		log.Printf("[ERROR] credo-ingest-article: failed to marshal request: %v for url %s", err, feedPost.Link)
		return event, nil // Don't fail the event publishing
	}

	// Log request size to help debug entity too large errors
	requestSize := len(buf)
	log.Printf("[DEBUG] credo-ingest-article: request size: %d bytes (content: %d bytes, excerpt: %d bytes, atomMeta present: %v) for url %s",
		requestSize, len(reqBody.Content), len(reqBody.Excerpt), reqBody.AtomMeta != nil, feedPost.Link)

	log.Printf("[DEBUG] credo-ingest-article: sending request JSON: %s", string(buf))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewReader(buf))
	if err != nil {
		log.Printf("[ERROR] credo-ingest-article: failed to create request: %v for url %s", err, feedPost.Link)
		return event, nil // Don't fail the event publishing
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range h.headers {
		if strings.HasPrefix(v, "$") {
			val := os.Getenv(v[1:])
			if val == "" {
				log.Printf("[ERROR] credo-ingest-article: header specified env, but variable not found: %s for url %s", v, feedPost.Link)
				return event, nil // Don't fail the event publishing
			}
			v = val
		}
		req.Header.Set(k, v)
	}

	log.Printf("[DEBUG] credo-ingest-article request: url=%s, title=%s, contentLength=%d, feedType=%s, hasAtomMeta=%v",
		feedPost.Link, feedPost.Title, len(feedPost.Content), feedPost.FeedType,
		reqBody.AtomMeta != nil)

	resp, err := h.client.Do(req)
	if err != nil {
		log.Printf("[ERROR] credo-ingest-article: request failed: %v for url %s", err, feedPost.Link)
		// Increment retry count on failure
		h.retryTracker.incrementRetryCount(feedPost.Link)

		// Check if we've exceeded retry limit
		retryCount := h.retryTracker.getRetryCount(feedPost.Link)
		if retryCount >= h.retryLimit {
			log.Printf("[WARN] credo-ingest-article: skipping analysis for url %s - retry count %d exceeds limit %d", feedPost.Link, retryCount, h.retryLimit)
			// Return event without error to allow publishing to continue
			return event, nil
		}

		return event, err
	}
	defer resp.Body.Close()

	log.Printf("[DEBUG] credo-ingest-article: response status: %s, headers: %v", resp.Status, resp.Header)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var respMsg string
		if resp.Body != nil {
			bodyBytes, _ := io.ReadAll(resp.Body)
			respMsg = string(bodyBytes)
			log.Printf("[ERROR] credo-ingest-article non-2xx response (%s): %s for url %s", resp.Status, respMsg, feedPost.Link)
		}

		// Check if this is a "request entity too large" error - treat as permanent failure
		if resp.StatusCode == 500 && strings.Contains(strings.ToLower(respMsg), "request entity too large") {
			log.Printf("[WARN] credo-ingest-article: skipping permanently - request entity too large for url %s", feedPost.Link)
			// Mark as permanently failed by setting retry count to max
			for i := 0; i < h.retryLimit; i++ {
				h.retryTracker.incrementRetryCount(feedPost.Link)
			}
			// Return event without error to allow publishing to continue
			return event, nil
		}

		// Increment retry count on failure
		h.retryTracker.incrementRetryCount(feedPost.Link)

		// Check if we've exceeded retry limit
		retryCount := h.retryTracker.getRetryCount(feedPost.Link)
		if retryCount >= h.retryLimit {
			log.Printf("[WARN] credo-ingest-article: skipping analysis for url %s - retry count %d exceeds limit %d", feedPost.Link, retryCount, h.retryLimit)
			// Return event without error to allow publishing to continue
			return event, nil
		}

		return event, errors.New("credo-ingest-article returned non-2xx status: " + resp.Status)
	}

	// Read the full response body for debugging
	bodyBytes, _ := io.ReadAll(resp.Body)
	log.Printf("[DEBUG] credo-ingest-article: raw response body: %s", string(bodyBytes))

	var out credoIngestArticleResponse
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		log.Printf("[ERROR] credo-ingest-article: failed to decode response: %v for url %s", err, feedPost.Link)
		log.Printf("[ERROR] credo-ingest-article: raw response was: %s", string(bodyBytes))
		// Increment retry count on failure
		h.retryTracker.incrementRetryCount(feedPost.Link)

		// Check if we've exceeded retry limit
		retryCount := h.retryTracker.getRetryCount(feedPost.Link)
		if retryCount >= h.retryLimit {
			log.Printf("[WARN] credo-ingest-article: skipping analysis for url %s - retry count %d exceeds limit %d", feedPost.Link, retryCount, h.retryLimit)
			// Return event without error to allow publishing to continue
			return event, nil
		}

		return event, err
	}

	log.Printf("[DEBUG] credo-ingest-article: parsed response - success: %t, message: %s", out.Success, out.Message)
	log.Printf("[DEBUG] credo-ingest-article: result - articleId: %s, features: %v", out.ArticleId, out.Features)

	if !out.Success {
		log.Printf("[ERROR] credo-ingest-article returned error: %s for url %s", out.Message, feedPost.Link)
		// Increment retry count on failure
		h.retryTracker.incrementRetryCount(feedPost.Link)

		// Check if we've exceeded retry limit
		retryCount := h.retryTracker.getRetryCount(feedPost.Link)
		if retryCount >= h.retryLimit {
			log.Printf("[WARN] credo-ingest-article: skipping analysis for url %s - retry count %d exceeds limit %d", feedPost.Link, retryCount, h.retryLimit)
			// Return event without error to allow publishing to continue
			return event, nil
		}

		return event, errors.New("credo-ingest-article returned error: " + out.Message)
	}

	// Reset retry count on success
	h.retryTracker.clearRetryCount(feedPost.Link)

	// Log successful processing
	log.Printf("[DEBUG] credo-ingest-article: full response structure - articleId: %s, features: %v for url %s", out.ArticleId, out.Features, feedPost.Link)

	if out.ArticleId != "" {
		log.Printf("[INFO] credo-ingest-article: article created with ID: %s for url %s", out.ArticleId, feedPost.Link)
	} else {
		log.Printf("[INFO] credo-ingest-article: article processed (no ID - not persisted) for url %s", feedPost.Link)
	}

	// Extract and add topic hashtags as "t" tags from API response
	updated := *event
	tagsAdded := false

	// Replace tags array with tags from API response if available
	if len(out.Tags) > 0 {
		// Clear existing "t" tags and replace with API-provided tags
		var newTags []nostr.Tag

		// Keep non-"t" tags
		for _, tag := range updated.Tags {
			if len(tag) >= 2 && tag[0] == "t" {
				// Skip existing "t" tags - they will be replaced
				continue
			}
			newTags = append(newTags, tag)
		}

		// Add new tags from API response (strip # prefix)
		addedCount := 0
		for _, tag := range out.Tags {
			// Strip # prefix and clean up
			cleanTag := strings.TrimSpace(strings.TrimPrefix(tag, "#"))
			if cleanTag == "" {
				continue
			}
			newTags = append(newTags, nostr.Tag{"t", cleanTag})
			addedCount++
		}

		updated.Tags = newTags

		log.Printf("[INFO] credo-ingest-article: replaced event tags with %d API-provided tags for url %s", addedCount, feedPost.Link)
		tagsAdded = true
	} else {
		log.Printf("[WARN] credo-ingest-article: no tags provided by API for url %s", feedPost.Link)
	}

	// Add AI assist results as "alt" tags with JSON-encoded feature objects
	if out.Assists != nil && out.Assists.GetFeatures != nil {
		features := out.Assists.GetFeatures

		// Get existing "alt" tags to avoid duplicates
		existingAltTags := map[string]bool{}
		for _, tag := range updated.Tags {
			if len(tag) >= 2 && tag[0] == "alt" {
				existingAltTags[strings.TrimSpace(tag[1])] = true
			}
		}

		assistTagsAdded := 0

		// Add get-features assist as JSON
		if features != nil && !existingAltTags["aiAssist:get-features"] {
			if featuresJSON, err := json.Marshal(features); err == nil {
				updated.Tags = append(updated.Tags, nostr.Tag{"alt", "aiAssist:get-features", string(featuresJSON)})
				existingAltTags["aiAssist:get-features"] = true
				assistTagsAdded++
			} else {
				log.Printf("[WARN] credo-ingest-article: failed to marshal get-features: %v for url %s", err, feedPost.Link)
			}
		}

		// Add unbait assist as JSON if available
		if out.Assists.Unbait != nil {
			log.Printf("[DEBUG] credo-ingest-article: unbait assist found - clickbaitScore: %.1f, answer: %s for url %s", out.Assists.Unbait.ClickbaitScore, out.Assists.Unbait.UnbaitAnswer, feedPost.Link)

			if !existingAltTags["aiAssist:unbait"] {
				if unbaitJSON, err := json.Marshal(out.Assists.Unbait); err == nil {
					updated.Tags = append(updated.Tags, nostr.Tag{"alt", "aiAssist:unbait", string(unbaitJSON)})
					existingAltTags["aiAssist:unbait"] = true
					assistTagsAdded++
				} else {
					log.Printf("[WARN] credo-ingest-article: failed to marshal unbait: %v for url %s", err, feedPost.Link)
				}
			}
		}

		if assistTagsAdded > 0 {
			log.Printf("[INFO] credo-ingest-article: added %d AI assist tags to event for url %s", assistTagsAdded, feedPost.Link)
			tagsAdded = true
		}
	}

	if len(out.Features) > 0 {
		log.Printf("[INFO] credo-ingest-article: detected features: %v for url %s", out.Features, feedPost.Link)
	}

	if tagsAdded {
		return &updated, nil
	}

	// Return original event if no tags were added
	log.Printf("[DEBUG] credo-ingest-article: completed processing for event %s for url %s", event.ID, feedPost.Link)
	return event, nil
}

// buildAtomMeta constructs the atomMeta structure from native atom feed data
func buildAtomMeta(atomFeed *atom.Feed, atomEntry *atom.Entry) *credoAtomMeta {
	meta := &credoAtomMeta{}

	// Build feed structure
	if atomFeed != nil {
		feed := &credoAtomFeed{
			ID:       truncateString(atomFeed.ID, 512),
			Updated:  truncateString(atomFeed.Updated, 512),
			Language: truncateString(atomFeed.Language, 512),
			Icon:     truncateString(atomFeed.Icon, 512),
			Logo:     truncateString(atomFeed.Logo, 512),
		}

		// Title
		if atomFeed.Title != "" {
			feed.Title = &credoAtomText{
				Type:  "text",
				Value: truncateString(atomFeed.Title, 512),
			}
		}

		// Subtitle
		if atomFeed.Subtitle != "" {
			feed.Subtitle = &credoAtomText{
				Type:  "text",
				Value: truncateString(atomFeed.Subtitle, 512),
			}
		}

		// Rights
		if atomFeed.Rights != "" {
			feed.Rights = &credoAtomText{
				Type:  "text",
				Value: truncateString(atomFeed.Rights, 512),
			}
		}

		// Generator
		if atomFeed.Generator != nil {
			feed.Generator = &credoAtomGenerator{
				Value:   truncateString(atomFeed.Generator.Value, 512),
				URI:     truncateString(atomFeed.Generator.URI, 512),
				Version: truncateString(atomFeed.Generator.Version, 512),
			}
		}

		// Links
		if len(atomFeed.Links) > 0 {
			feed.Links = make([]*credoAtomLink, len(atomFeed.Links))
			for i, link := range atomFeed.Links {
				feed.Links[i] = &credoAtomLink{
					Href:     truncateString(link.Href, 512),
					Rel:      truncateString(link.Rel, 512),
					Type:     truncateString(link.Type, 512),
					Hreflang: truncateString(link.Hreflang, 512),
					Title:    truncateString(link.Title, 512),
					Length:   truncateString(link.Length, 512),
				}
			}
		}

		// Authors
		if len(atomFeed.Authors) > 0 {
			feed.Authors = make([]*credoAtomPerson, len(atomFeed.Authors))
			for i, author := range atomFeed.Authors {
				feed.Authors[i] = &credoAtomPerson{
					Name:  truncateString(author.Name, 512),
					Email: truncateString(author.Email, 512),
					URI:   truncateString(author.URI, 512),
				}
			}
		}

		// Contributors
		if len(atomFeed.Contributors) > 0 {
			feed.Contributors = make([]*credoAtomPerson, len(atomFeed.Contributors))
			for i, contributor := range atomFeed.Contributors {
				feed.Contributors[i] = &credoAtomPerson{
					Name:  truncateString(contributor.Name, 512),
					Email: truncateString(contributor.Email, 512),
					URI:   truncateString(contributor.URI, 512),
				}
			}
		}

		// Categories
		if len(atomFeed.Categories) > 0 {
			feed.Categories = make([]*credoAtomCategory, len(atomFeed.Categories))
			for i, category := range atomFeed.Categories {
				feed.Categories[i] = &credoAtomCategory{
					Term:   truncateString(category.Term, 512),
					Scheme: truncateString(category.Scheme, 512),
					Label:  truncateString(category.Label, 512),
				}
			}
		}

		meta.Feed = feed
	}

	// Build entry structure
	if atomEntry != nil {
		entry := &credoAtomEntry{
			ID:        truncateString(atomEntry.ID, 512),
			Updated:   truncateString(atomEntry.Updated, 512),
			Published: truncateString(atomEntry.Published, 512),
		}

		// Title
		if atomEntry.Title != "" {
			entry.Title = &credoAtomText{
				Type:  "text",
				Value: truncateString(atomEntry.Title, 512),
			}
		}

		// Summary
		if atomEntry.Summary != "" {
			entry.Summary = &credoAtomText{
				Type:  "text",
				Value: truncateString(atomEntry.Summary, 512),
			}
		}

		// Rights
		if atomEntry.Rights != "" {
			entry.Rights = &credoAtomText{
				Type:  "text",
				Value: truncateString(atomEntry.Rights, 512),
			}
		}

		// Content
		if atomEntry.Content != nil {
			contentType := "text"
			if atomEntry.Content.Type != "" {
				contentType = truncateString(atomEntry.Content.Type, 512)
			}
			entry.Content = &credoAtomContent{
				Type:  contentType,
				Value: truncateString(atomEntry.Content.Value, 512),
				Src:   truncateString(atomEntry.Content.Src, 512),
			}
		}

		// Authors
		if len(atomEntry.Authors) > 0 {
			entry.Authors = make([]*credoAtomPerson, len(atomEntry.Authors))
			for i, author := range atomEntry.Authors {
				entry.Authors[i] = &credoAtomPerson{
					Name:  truncateString(author.Name, 512),
					Email: truncateString(author.Email, 512),
					URI:   truncateString(author.URI, 512),
				}
			}
		}

		// Contributors
		if len(atomEntry.Contributors) > 0 {
			entry.Contributors = make([]*credoAtomPerson, len(atomEntry.Contributors))
			for i, contributor := range atomEntry.Contributors {
				entry.Contributors[i] = &credoAtomPerson{
					Name:  truncateString(contributor.Name, 512),
					Email: truncateString(contributor.Email, 512),
					URI:   truncateString(contributor.URI, 512),
				}
			}
		}

		// Categories
		if len(atomEntry.Categories) > 0 {
			entry.Categories = make([]*credoAtomCategory, len(atomEntry.Categories))
			for i, category := range atomEntry.Categories {
				entry.Categories[i] = &credoAtomCategory{
					Term:   truncateString(category.Term, 512),
					Scheme: truncateString(category.Scheme, 512),
					Label:  truncateString(category.Label, 512),
				}
			}
		}

		// Links
		if len(atomEntry.Links) > 0 {
			entry.Links = make([]*credoAtomLink, len(atomEntry.Links))
			for i, link := range atomEntry.Links {
				entry.Links[i] = &credoAtomLink{
					Href:     truncateString(link.Href, 512),
					Rel:      truncateString(link.Rel, 512),
					Type:     truncateString(link.Type, 512),
					Hreflang: truncateString(link.Hreflang, 512),
					Title:    truncateString(link.Title, 512),
					Length:   truncateString(link.Length, 512),
				}
			}
		}

		// Source
		if atomEntry.Source != nil {
			source := &credoAtomSource{
				ID:      truncateString(atomEntry.Source.ID, 512),
				Updated: truncateString(atomEntry.Source.Updated, 512),
				Icon:    truncateString(atomEntry.Source.Icon, 512),
				Logo:    truncateString(atomEntry.Source.Logo, 512),
			}

			if atomEntry.Source.Title != "" {
				source.Title = &credoAtomText{
					Type:  "text",
					Value: truncateString(atomEntry.Source.Title, 512),
				}
			}

			if atomEntry.Source.Subtitle != "" {
				source.Subtitle = &credoAtomText{
					Type:  "text",
					Value: truncateString(atomEntry.Source.Subtitle, 512),
				}
			}

			if atomEntry.Source.Rights != "" {
				source.Rights = &credoAtomText{
					Type:  "text",
					Value: truncateString(atomEntry.Source.Rights, 512),
				}
			}

			if atomEntry.Source.Generator != nil {
				source.Generator = &credoAtomGenerator{
					Value:   truncateString(atomEntry.Source.Generator.Value, 512),
					URI:     truncateString(atomEntry.Source.Generator.URI, 512),
					Version: truncateString(atomEntry.Source.Generator.Version, 512),
				}
			}

			if len(atomEntry.Source.Links) > 0 {
				source.Links = make([]*credoAtomLink, len(atomEntry.Source.Links))
				for i, link := range atomEntry.Source.Links {
					source.Links[i] = &credoAtomLink{
						Href:     truncateString(link.Href, 512),
						Rel:      truncateString(link.Rel, 512),
						Type:     truncateString(link.Type, 512),
						Hreflang: truncateString(link.Hreflang, 512),
						Title:    truncateString(link.Title, 512),
						Length:   truncateString(link.Length, 512),
					}
				}
			}

			if len(atomEntry.Source.Authors) > 0 {
				source.Authors = make([]*credoAtomPerson, len(atomEntry.Source.Authors))
				for i, author := range atomEntry.Source.Authors {
					source.Authors[i] = &credoAtomPerson{
						Name:  truncateString(author.Name, 512),
						Email: truncateString(author.Email, 512),
						URI:   truncateString(author.URI, 512),
					}
				}
			}

			if len(atomEntry.Source.Contributors) > 0 {
				source.Contributors = make([]*credoAtomPerson, len(atomEntry.Source.Contributors))
				for i, contributor := range atomEntry.Source.Contributors {
					source.Contributors[i] = &credoAtomPerson{
						Name:  truncateString(contributor.Name, 512),
						Email: truncateString(contributor.Email, 512),
						URI:   truncateString(contributor.URI, 512),
					}
				}
			}

			if len(atomEntry.Source.Categories) > 0 {
				source.Categories = make([]*credoAtomCategory, len(atomEntry.Source.Categories))
				for i, category := range atomEntry.Source.Categories {
					source.Categories[i] = &credoAtomCategory{
						Term:   truncateString(category.Term, 512),
						Scheme: truncateString(category.Scheme, 512),
						Label:  truncateString(category.Label, 512),
					}
				}
			}

			entry.Source = source
		}

		meta.Item = entry
	}

	return meta
}

// buildAtomMetaFromRSS maps RSS feed data to atom format for API compatibility
func buildAtomMetaFromRSS(rssFeed *rss.Feed, rssItem *rss.Item) *credoAtomMeta {
	meta := &credoAtomMeta{}

	// Build feed structure from RSS
	if rssFeed != nil {
		feed := &credoAtomFeed{
			ID:       truncateString(rssFeed.Link, 512),
			Updated:  truncateString(rssFeed.LastBuildDate, 512),
			Language: truncateString(rssFeed.Language, 512),
		}

		// Title
		if rssFeed.Title != "" {
			feed.Title = &credoAtomText{
				Type:  "text",
				Value: truncateString(rssFeed.Title, 512),
			}
		}

		// Description as subtitle
		if rssFeed.Description != "" {
			feed.Subtitle = &credoAtomText{
				Type:  "text",
				Value: truncateString(rssFeed.Description, 512),
			}
		}

		// Generator
		if rssFeed.Generator != "" {
			feed.Generator = &credoAtomGenerator{
				Value: truncateString(rssFeed.Generator, 512),
			}
		}

		// Categories
		if len(rssFeed.Categories) > 0 {
			feed.Categories = make([]*credoAtomCategory, len(rssFeed.Categories))
			for i, category := range rssFeed.Categories {
				feed.Categories[i] = &credoAtomCategory{
					Term:   truncateString(category.Value, 512),
					Scheme: truncateString(category.Domain, 512),
				}
			}
		}

		// Image as logo
		if rssFeed.Image != nil {
			feed.Logo = truncateString(rssFeed.Image.URL, 512)
		}

		meta.Feed = feed
	}

	// Build entry structure from RSS item
	if rssItem != nil {
		entry := &credoAtomEntry{
			ID:      truncateString(rssItem.Link, 512),
			Updated: truncateString(rssItem.PubDate, 512),
		}

		// Title
		if rssItem.Title != "" {
			entry.Title = &credoAtomText{
				Type:  "text",
				Value: truncateString(rssItem.Title, 512),
			}
		}

		// Description as summary
		if rssItem.Description != "" {
			entry.Summary = &credoAtomText{
				Type:  "text",
				Value: truncateString(rssItem.Description, 512),
			}
		}

		// Content
		if rssItem.Content != "" {
			entry.Content = &credoAtomContent{
				Type:  "text",
				Value: truncateString(rssItem.Content, 512),
			}
		}

		// Author
		if rssItem.Author != "" {
			entry.Authors = []*credoAtomPerson{
				{
					Name: truncateString(rssItem.Author, 512),
				},
			}
		}

		// Categories
		if len(rssItem.Categories) > 0 {
			entry.Categories = make([]*credoAtomCategory, len(rssItem.Categories))
			for i, category := range rssItem.Categories {
				entry.Categories[i] = &credoAtomCategory{
					Term:   truncateString(category.Value, 512),
					Scheme: truncateString(category.Domain, 512),
				}
			}
		}

		// Enclosures as links
		if len(rssItem.Enclosures) > 0 {
			entry.Links = make([]*credoAtomLink, len(rssItem.Enclosures))
			for i, enclosure := range rssItem.Enclosures {
				entry.Links[i] = &credoAtomLink{
					Href:   truncateString(enclosure.URL, 512),
					Rel:    "enclosure",
					Type:   truncateString(enclosure.Type, 512),
					Length: truncateString(enclosure.Length, 512),
				}
			}
		}

		meta.Item = entry
	}

	return meta
}
