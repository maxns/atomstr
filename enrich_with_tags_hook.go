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
	"strings"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

// EnrichWithTagsHook calls a SuggestTags API to get tag suggestions for a Nostr event
// and appends them as "t" tags to the event before signing/publishing.
type EnrichWithTagsHook struct {
	url     string
	headers map[string]string
	client  *http.Client
}

func NewEnrichWithTagsHook(endpoint string, headers map[string]string) *EnrichWithTagsHook {
	return &EnrichWithTagsHook{
		url:     endpoint,
		headers: headers,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// SuggestTagsRequest / Response per hooks/SuggestTags.spec.md
type suggestTagsRequest struct {
	Params struct {
		Note         string   `json:"note,omitempty"`
		ExistingTags []string `json:"existingTags,omitempty"`
		SearchText   string   `json:"searchText,omitempty"`
		ByEvent      *struct {
			Event        nostr.Event `json:"event"`
			LinkedEvents []any       `json:"linkedEvents,omitempty"`
		} `json:"byEvent,omitempty"`
		ByEventId *struct {
			Id        string   `json:"id"`
			RelayUrls []string `json:"relayUrls,omitempty"`
		} `json:"byEventId,omitempty"`
	} `json:"params"`
	Options struct {
		MaxTags int         `json:"maxTags,omitempty"`
		Retry   interface{} `json:"retry,omitempty"`
	} `json:"options"`
	Hints  []string `json:"hints,omitempty"`
	TimeMs int64    `json:"timeMs,omitempty"`
}

type suggestTagsResponse struct {
	Success bool `json:"success"`
	Result  *struct {
		Tags []string `json:"tags"`
	} `json:"result"`
	Message string `json:"message,omitempty"`
	TimeMs  int64  `json:"timeMs,omitempty"`
}

func (h *EnrichWithTagsHook) BeforePublish(ctx context.Context, feed feedStruct, feedPost feedPostStruct, event *nostr.Event) (*nostr.Event, error) {
	// Build request: include byEvent with the full event and pass note as well
	reqBody := suggestTagsRequest{}
	reqBody.Params.Note = event.Content
	reqBody.Params.ExistingTags = extractCurrentTags(event)
	reqBody.Params.ByEvent = &struct {
		Event        nostr.Event `json:"event"`
		LinkedEvents []any       `json:"linkedEvents,omitempty"`
	}{Event: *event}
	reqBody.TimeMs = time.Now().UnixMilli()

	buf, err := json.Marshal(&reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range h.headers {
		if strings.HasPrefix(v, "$") {
			val := os.Getenv(v[1:])
			if val == "" {
				return nil, errors.New("header specified env, but variable not found: " + v)
			}
			v = val
		}
		req.Header.Set(k, v)
	}
	log.Printf("[DEBUG] enrich-with-tags request: %s", string(buf))

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var respMsg string
		if resp.Body != nil {
			bodyBytes, _ := io.ReadAll(resp.Body)
			respMsg = string(bodyBytes)
			log.Printf("[DEBUG] enrich-with-tags non-2xx response: %s", respMsg)
			// Reset body for later error handling if needed
			resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		}
		return nil, errors.New("enrich-with-tags returned non-2xx status: " + resp.Status)
	}
	defer resp.Body.Close()

	var out suggestTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if !out.Success {
		return nil, errors.New("enrich-with-tags returned error: " + out.Message)
	}
	if out.Result == nil || len(out.Result.Tags) == 0 {
		log.Printf("[DEBUG] enrich-with-tags returned no tags")
		return event, nil
	}

	updated := *event
	// Merge suggested tags as "t" tags, deduplicate
	existing := map[string]bool{}
	for _, tag := range updated.Tags {
		if len(tag) >= 2 && tag[0] == "t" {
			existing[strings.TrimSpace(tag[1])] = true
		}
	}
	for _, t := range out.Result.Tags {
		k := strings.TrimSpace(t)
		if k == "" {
			continue
		}
		if !existing[k] {
			updated.Tags = append(updated.Tags, nostr.Tag{"t", k})
			existing[k] = true
		}
	}

	log.Printf("[DEBUG] enrich-with-tags added %d tags", len(out.Result.Tags))
	return &updated, nil
}

func extractCurrentTags(event *nostr.Event) []string {
	var tags []string
	for _, tag := range event.Tags {
		if len(tag) >= 2 && tag[0] == "t" {
			v := strings.TrimSpace(tag[1])
			if v != "" {
				tags = append(tags, v)
			}
		}
	}
	return tags
}
