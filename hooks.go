package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

// NostrEventHook defines a hook invoked before an event is signed/published.
// It can modify the event or return an error to abort publishing.
type NostrEventHook interface {
	BeforePublish(ctx context.Context, feed feedStruct, feedPost feedPostStruct, event *nostr.Event) (*nostr.Event, error)
}

// RegisterPrePublishHook appends a hook to the Atomstr instance.
func (a *Atomstr) RegisterPrePublishHook(h NostrEventHook) {
	a.prePublishHooks = append(a.prePublishHooks, h)
}

// runPrePublishHooks executes hooks sequentially, passing the event through.
func (a *Atomstr) runPrePublishHooks(ctx context.Context, feed feedStruct, post feedPostStruct, ev *nostr.Event) (*nostr.Event, error) {
	current := ev
	for _, h := range a.prePublishHooks {
		updated, err := h.BeforePublish(ctx, feed, post, current)
		if err != nil {
			return nil, err
		}
		if updated == nil {
			return nil, errors.New("hook returned nil event")
		}
		current = updated
	}
	return current, nil
}

// RestEnrichHook calls an external REST endpoint to enrich a Nostr event.
// It sends feedItem and nostrEvent and expects {result:"success"|"error", nostrEvent:{...}}.
type RestEnrichHook struct {
	url     string
	method  string
	headers map[string]string
	client  *http.Client
	compose string // jsonBody | queryParams
	parse   string // jsonParse
}

func NewRestEnrichHook(rawURL, method string, headers map[string]string, compose, parse string) *RestEnrichHook {
	m := strings.ToUpper(strings.TrimSpace(method))
	if m == "" {
		m = "POST"
	}
	if compose == "" {
		if m == http.MethodGet {
			compose = "queryParams"
		} else {
			compose = "jsonBody"
		}
	}
	if parse == "" {
		parse = "jsonParse"
	}
	return &RestEnrichHook{
		url:     rawURL,
		method:  m,
		headers: headers,
		client:  &http.Client{Timeout: 10 * time.Second},
		compose: compose,
		parse:   parse,
	}
}

type restHookRequest struct {
	Feed       feedStruct     `json:"feed"`
	FeedPost   feedPostStruct `json:"feedPost"`
	NostrEvent nostr.Event    `json:"nostrEvent"`
}

type restHookResponse struct {
	Result     string       `json:"result"`
	NostrEvent *nostr.Event `json:"nostrEvent"`
}

func (h *RestEnrichHook) BeforePublish(ctx context.Context, feed feedStruct, feedPost feedPostStruct, event *nostr.Event) (*nostr.Event, error) {
	payload := restHookRequest{Feed: feed, FeedPost: feedPost, NostrEvent: *event}

	var req *http.Request
	var err error

	if h.compose == "queryParams" {
		feedJSON, err := json.Marshal(payload.Feed)
		if err != nil {
			return nil, err
		}
		postJSON, err := json.Marshal(payload.FeedPost)
		if err != nil {
			return nil, err
		}
		evJSON, err := json.Marshal(payload.NostrEvent)
		if err != nil {
			return nil, err
		}
		u, err := url.Parse(h.url)
		if err != nil {
			return nil, err
		}
		q := u.Query()
		q.Set("feed", string(feedJSON))
		q.Set("feedPost", string(postJSON))
		q.Set("nostrEvent", string(evJSON))
		u.RawQuery = q.Encode()
		method := h.method
		if method == "" {
			method = http.MethodGet
		}
		req, err = http.NewRequestWithContext(ctx, method, u.String(), nil)
		if err != nil {
			return nil, err
		}
	} else {
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		req, err = http.NewRequestWithContext(ctx, h.method, h.url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
	}

	for k, v := range h.headers {
		if strings.HasPrefix(v, "$") {
			v = os.Getenv(v[1:])

			if v == "" {
				return nil, errors.New("header specifed an env, but env variable not found: " + v)
			}
		}
		req.Header.Set(k, v)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("rest hook returned non-2xx status")
	}

	if h.parse == "jsonParse" {
		var out restHookResponse
		dec := json.NewDecoder(resp.Body)
		if err := dec.Decode(&out); err != nil {
			return nil, err
		}
		if strings.ToLower(out.Result) != "success" {
			return nil, errors.New("rest hook returned error result")
		}
		if out.NostrEvent == nil {
			log.Println("[WARN] REST hook success but no nostrEvent in response; using original event")
			return event, nil
		}
		return out.NostrEvent, nil
	}
	// Default parser: passthrough
	return event, nil
}
