package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

// Register this hook in the custom hook registry
func init() {
	RegisterCustomHook("credoIngestArticle", NewCredoIngestArticleHookFromConfig)
}

// CredoIngestArticleHook calls the Credo API to ingest articles for AI analysis
// It extracts article content from Nostr events and sends it to the Credo API
type CredoIngestArticleHook struct {
	url     string
	headers map[string]string
	client  *http.Client
}

func NewCredoIngestArticleHook(endpoint string, headers map[string]string) *CredoIngestArticleHook {
	return &CredoIngestArticleHook{
		url:     endpoint,
		headers: headers,
		client:  &http.Client{Timeout: 30 * time.Second}, // Longer timeout for AI processing
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

	return NewCredoIngestArticleHook(url, headers), nil
}

// CredoIngestArticleRequest per API specification
type credoIngestArticleRequest struct {
	URL      string `json:"url"`
	Title    string `json:"title,omitempty"`
	Content  string `json:"content"`
	Language string `json:"language,omitempty"`
}

type credoIngestArticleResponse struct {
	Success bool `json:"success"`
	Result  *struct {
		ArticleId string   `json:"articleId,omitempty"`
		Features  []string `json:"features,omitempty"`
		Assists   *struct {
			GetFeatures *struct {
				Features                      []string `json:"features"`
				EstimatedReadTimeMinutes      int      `json:"estimatedReadTimeMinutes"`
				TopicHashTags                 []string `json:"topicHashTags"`
				Clarity                       string   `json:"clarity"`
				LanguageCode                  string   `json:"languageCode"`
				Tone                          string   `json:"tone"`
				Length                        string   `json:"length"`
				Complexity                    string   `json:"complexity"`
				AnalysisSummaryInUserLanguage string   `json:"analysisSummaryInUserLanguage"`
			} `json:"getFeatures,omitempty"`
			Unbait *struct {
				ClickbaitScore int    `json:"clickbaitScore"`
				Feature        string `json:"feature"`
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
		Message string `json:"message"`
	} `json:"result"`
	Message string `json:"message,omitempty"`
	TimeMs  int64  `json:"timeMs,omitempty"`
}

// URL regex pattern to extract URLs from content
var urlRegex = regexp.MustCompile(`https?://[^\s]+`)

func (h *CredoIngestArticleHook) BeforePublish(ctx context.Context, feed feedStruct, feedPost feedPostStruct, event *nostr.Event) (*nostr.Event, error) {
	log.Printf("[DEBUG] credo-ingest-article: processing event %s with content length %d", event.ID, len(event.Content))

	// Extract URLs from the event content
	urls := extractURLsFromContent(event.Content)
	log.Printf("[DEBUG] credo-ingest-article: found %d URLs in content", len(urls))
	if len(urls) == 0 {
		log.Printf("[DEBUG] credo-ingest-article: no URLs found in event content, skipping")
		return event, nil
	}

	// Use the first URL found
	articleURL := urls[0]

	// Extract title from content (first line or derive from URL)
	title := extractTitleFromContent(event.Content, articleURL)

	// Build request
	reqBody := credoIngestArticleRequest{}
	reqBody.URL = articleURL
	reqBody.Title = title
	reqBody.Content = event.Content
	reqBody.Language = "en" // Default to English

	buf, err := json.Marshal(&reqBody)
	if err != nil {
		log.Printf("[ERROR] credo-ingest-article: failed to marshal request: %v", err)
		return event, nil // Don't fail the event publishing
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewReader(buf))
	if err != nil {
		log.Printf("[ERROR] credo-ingest-article: failed to create request: %v", err)
		return event, nil // Don't fail the event publishing
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range h.headers {
		if strings.HasPrefix(v, "$") {
			val := os.Getenv(v[1:])
			if val == "" {
				log.Printf("[ERROR] credo-ingest-article: header specified env, but variable not found: %s", v)
				return event, nil // Don't fail the event publishing
			}
			v = val
		}
		req.Header.Set(k, v)
	}

	log.Printf("[DEBUG] credo-ingest-article request: URL=%s, Title=%s", articleURL, title)

	resp, err := h.client.Do(req)
	if err != nil {
		log.Printf("[ERROR] credo-ingest-article: request failed: %v", err)
		return event, nil // Don't fail the event publishing
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var respMsg string
		if resp.Body != nil {
			bodyBytes, _ := io.ReadAll(resp.Body)
			respMsg = string(bodyBytes)
			log.Printf("[ERROR] credo-ingest-article non-2xx response (%s): %s", resp.Status, respMsg)
		}
		return event, nil // Don't fail the event publishing
	}

	var out credoIngestArticleResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		log.Printf("[ERROR] credo-ingest-article: failed to decode response: %v", err)
		return event, nil // Don't fail the event publishing
	}

	if !out.Success {
		log.Printf("[ERROR] credo-ingest-article returned error: %s", out.Message)
		return event, nil // Don't fail the event publishing
	}

	// Log successful processing and extract tags
	if out.Result != nil {
		if out.Result.ArticleId != "" {
			log.Printf("[INFO] credo-ingest-article: article created with ID: %s", out.Result.ArticleId)
		} else {
			log.Printf("[INFO] credo-ingest-article: article processed (no ID - not persisted)")
		}

		if out.Result.Assists != nil && out.Result.Assists.GetFeatures != nil {
			features := out.Result.Assists.GetFeatures
			log.Printf("[INFO] credo-ingest-article: analysis complete - tags: %v, readTime: %dm, clarity: %s",
				features.TopicHashTags, features.EstimatedReadTimeMinutes, features.Clarity)

			// Extract and add topic hashtags as "t" tags and unbait data as "alt" tags
			updated := *event
			tagsAdded := false

			// Add topic hashtags as "t" tags
			if len(features.TopicHashTags) > 0 {
				// Get existing "t" tags to avoid duplicates
				existingTTags := map[string]bool{}
				for _, tag := range updated.Tags {
					if len(tag) >= 2 && tag[0] == "t" {
						existingTTags[strings.TrimSpace(tag[1])] = true
					}
				}

				// Add new tags from topic hashtags (strip # prefix)
				addedCount := 0
				for _, hashtag := range features.TopicHashTags {
					// Strip # prefix and clean up
					tag := strings.TrimSpace(strings.TrimPrefix(hashtag, "#"))
					if tag == "" {
						continue
					}
					// Add if not already present
					if !existingTTags[tag] {
						updated.Tags = append(updated.Tags, nostr.Tag{"t", tag})
						existingTTags[tag] = true
						addedCount++
					}
				}

				if addedCount > 0 {
					log.Printf("[INFO] credo-ingest-article: added %d topic tags to event", addedCount)
					tagsAdded = true
				}
			}

			// Add AI assist results as "alt" tags with JSON-encoded feature objects
			if out.Result.Assists != nil {
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
						log.Printf("[WARN] credo-ingest-article: failed to marshal get-features: %v", err)
					}
				}

				// Add unbait assist as JSON if available
				if out.Result.Assists.Unbait != nil && !existingAltTags["aiAssist:unbait"] {
					if unbaitJSON, err := json.Marshal(out.Result.Assists.Unbait); err == nil {
						updated.Tags = append(updated.Tags, nostr.Tag{"alt", "aiAssist:unbait", string(unbaitJSON)})
						existingAltTags["aiAssist:unbait"] = true
						assistTagsAdded++
					} else {
						log.Printf("[WARN] credo-ingest-article: failed to marshal unbait: %v", err)
					}
				}

				if assistTagsAdded > 0 {
					log.Printf("[INFO] credo-ingest-article: added %d AI assist tags to event", assistTagsAdded)
					tagsAdded = true
				}
			}

			if tagsAdded {
				return &updated, nil
			}
		}

		if len(out.Result.Features) > 0 {
			log.Printf("[INFO] credo-ingest-article: detected features: %v", out.Result.Features)
		}
	}

	// Return original event if no tags were added
	log.Printf("[DEBUG] credo-ingest-article: completed processing for event %s", event.ID)
	return event, nil
}

// extractURLsFromContent extracts HTTP/HTTPS URLs from text content
func extractURLsFromContent(content string) []string {
	matches := urlRegex.FindAllString(content, -1)
	var urls []string
	for _, match := range matches {
		// Clean up URL (remove trailing punctuation)
		cleaned := strings.TrimRight(match, ".,!?;:")
		if isValidURL(cleaned) {
			urls = append(urls, cleaned)
		}
	}
	return urls
}

// isValidURL checks if a string is a valid URL
func isValidURL(str string) bool {
	u, err := url.Parse(str)
	return err == nil && u.Scheme != "" && u.Host != ""
}

// extractTitleFromContent attempts to extract a title from content
func extractTitleFromContent(content, articleURL string) string {
	lines := strings.Split(strings.TrimSpace(content), "\n")

	// Look for the first non-URL line as potential title
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip if the line is just the URL
		if strings.Contains(line, articleURL) && len(line) < len(articleURL)+20 {
			continue
		}
		// Use this line as title if it's not too long and doesn't look like a URL
		if len(line) > 10 && len(line) < 200 && !urlRegex.MatchString(line) {
			return line
		}
	}

	// Fallback: derive title from URL
	if u, err := url.Parse(articleURL); err == nil {
		// Use the path or host as fallback title
		if u.Path != "" && u.Path != "/" {
			path := strings.Trim(u.Path, "/")
			// Convert dashes/underscores to spaces and title case
			title := strings.ReplaceAll(path, "-", " ")
			title = strings.ReplaceAll(title, "_", " ")
			if len(title) > 0 {
				return strings.Title(title)
			}
		}
		return u.Host
	}

	return "" // Empty title as fallback
}
