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
	Excerpt  string `json:"excerpt,omitempty"`
	Language string `json:"language,omitempty"`
	GUID     string `json:"GUID,omitempty"`
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

	// Build request
	reqBody := credoIngestArticleRequest{}
	reqBody.URL = articleURL
	reqBody.Title = feedPost.Title
	reqBody.Content = "" // We don't have full article content, so pass empty
	reqBody.Excerpt = feedPost.Description
	reqBody.Language = "en" // Default to English
	reqBody.GUID = feedPost.GUID

	buf, err := json.Marshal(&reqBody)
	if err != nil {
		log.Printf("[ERROR] credo-ingest-article: failed to marshal request: %v", err)
		return event, nil // Don't fail the event publishing
	}

	log.Printf("[DEBUG] credo-ingest-article: sending request JSON: %s", string(buf))

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

	log.Printf("[DEBUG] credo-ingest-article request: URL=%s, Title=%s", articleURL, feedPost.Title)

	resp, err := h.client.Do(req)
	if err != nil {
		log.Printf("[ERROR] credo-ingest-article: request failed: %v", err)
		return event, err
	}
	defer resp.Body.Close()

	log.Printf("[DEBUG] credo-ingest-article: response status: %s, headers: %v", resp.Status, resp.Header)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var respMsg string
		if resp.Body != nil {
			bodyBytes, _ := io.ReadAll(resp.Body)
			respMsg = string(bodyBytes)
			log.Printf("[ERROR] credo-ingest-article non-2xx response (%s): %s", resp.Status, respMsg)
		}
		return event, errors.New("credo-ingest-article returned non-2xx status: " + resp.Status)
	}

	// Read the full response body for debugging
	bodyBytes, _ := io.ReadAll(resp.Body)
	log.Printf("[DEBUG] credo-ingest-article: raw response body: %s", string(bodyBytes))

	var out credoIngestArticleResponse
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		log.Printf("[ERROR] credo-ingest-article: failed to decode response: %v", err)
		log.Printf("[ERROR] credo-ingest-article: raw response was: %s", string(bodyBytes))
		return event, err
	}

	log.Printf("[DEBUG] credo-ingest-article: parsed response - success: %t, message: %s", out.Success, out.Message)
	log.Printf("[DEBUG] credo-ingest-article: result - articleId: %s, features: %v", out.ArticleId, out.Features)

	if !out.Success {
		log.Printf("[ERROR] credo-ingest-article returned error: %s", out.Message)
		return event, errors.New("credo-ingest-article returned error: " + out.Message)
	}

	// Log successful processing and extract tags
	log.Printf("[DEBUG] credo-ingest-article: full response structure - articleId: %s, features: %v", out.ArticleId, out.Features)

	if out.ArticleId != "" {
		log.Printf("[INFO] credo-ingest-article: article created with ID: %s", out.ArticleId)
	} else {
		log.Printf("[INFO] credo-ingest-article: article processed (no ID - not persisted)")
	}

	if out.Assists != nil && out.Assists.GetFeatures != nil {
		features := out.Assists.GetFeatures
		log.Printf("[DEBUG] credo-ingest-article: assists structure: %+v", out.Assists)
		log.Printf("[DEBUG] credo-ingest-article: getFeatures - features: %v, readTime: %.1fm, hashtags: %v, clarity: %s, tone: %s, language: %s", features.Features, features.EstimatedReadTimeMinutes, features.TopicHashTags, features.Clarity, features.Tone, features.LanguageCode)
		log.Printf("[INFO] credo-ingest-article: analysis complete - tags: %v, readTime: %.1fm, clarity: %s", features.TopicHashTags, features.EstimatedReadTimeMinutes, features.Clarity)

		// Extract and add topic hashtags as "t" tags and unbait data as "alt" tags
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

			log.Printf("[INFO] credo-ingest-article: replaced event tags with %d API-provided tags", addedCount)
			tagsAdded = true
		} else {
			log.Printf("[WARN] credo-ingest-article: no tags provided by API")
		}

		// Add AI assist results as "alt" tags with JSON-encoded feature objects
		if out.Assists != nil {
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
			if out.Assists.Unbait != nil {
				log.Printf("[DEBUG] credo-ingest-article: unbait assist found - clickbaitScore: %.1f, answer: %s", out.Assists.Unbait.ClickbaitScore, out.Assists.Unbait.UnbaitAnswer)

				if !existingAltTags["aiAssist:unbait"] {
					if unbaitJSON, err := json.Marshal(out.Assists.Unbait); err == nil {
						updated.Tags = append(updated.Tags, nostr.Tag{"alt", "aiAssist:unbait", string(unbaitJSON)})
						existingAltTags["aiAssist:unbait"] = true
						assistTagsAdded++
					} else {
						log.Printf("[WARN] credo-ingest-article: failed to marshal unbait: %v", err)
					}
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

	if len(out.Features) > 0 {
		log.Printf("[INFO] credo-ingest-article: detected features: %v", out.Features)
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
