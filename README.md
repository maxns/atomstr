# atomstr

atomstr is a RSS/Atom gateway to Nostr.

It fetches all sorts of RSS or Atom feeds, generates Nostr profiles for each and posts new entries to given Nostr relay(s). If you have one of these relays in your profile, you can find and subscribe to the feeds.

Although self hosting is preferable (it always is), there's a test instance at [https://atomstr.data.haus](https://atomstr.data.haus) - please don't hammer this too much as it is running next to my desk.

## Features

- Web portal to add feeds
- Automatic NIP-05 verification of profiles
- Parallel scraping of feeds
- Easy installation
- NIP-48 support

## Installation / Configuration

The prefered way to run this is via Docker. Just use the included [docker-compose.yaml](https://git.sr.ht/~psic4t/atomstr/blob/master/docker-compose.yaml) and modify it to your needs. It contains ready to run Traefik labels. You can remove this part, if you are using ngnix or HAproxy.

If you want to compile it yourself just run "make". 


## Configuration

All configuration is done via environment variables. If you don't want this, modify defines.go.

The following variables are available:

- `DB_PATH`, "./atomstr.db"
- `FETCH_INTERVAL` refresh interval for feeds, default "15m"
- `METADATA_INTERVAL` refresh interval for feed name, icon, etc, default "2h"
- `MAX_POST_AGE` maximum age of posts to publish, default "72h"
- `LOG_LEVEL`, "INFO"
- `WEBSERVER_PORT`, "8061"
- `NIP05_DOMAIN` webserver domain, default  "atomstr.data.haus"
- `MAX_WORKERS` max work in paralel. Default "5"
- `RELAYS_TO_PUBLISH_TO` to which relays this server posts to, add more comma separated. Default "wss://nostr.data.haus"
- `DEFAULT_FEED_IMAGE` if no feed image is found, use this. Default "https://void.cat/d/NDrSDe4QMx9jh6bD9LJwcK"

### Hooks configuration (YAML)

Hooks are configured via a YAML file placed next to the binary as `hooks.yaml` (or path set via `HOOKS_CONFIG_PATH`). Hooks run at specific lifecycle stages, e.g., before posting a Nostr event.

Basic structure:

```yaml
hooks:
  prePostNostrPublish:
    - name: enrichWithWenby
      type: restEnrich
      url: https://wenby.example.com/enrich
      method: POST
      headers:
        x-auth-token: "YOUR_TOKEN"
      composeRequestFunc: jsonBody   # or queryParams
      parseResponseFunc: jsonParse   # reserved for future parsers
  preNostrProfilePublish: []
```

Supported hook types:
- `restEnrich`: calls a REST endpoint to enrich/modify the Nostr event before signing/publishing.

REST request formats:
- When `composeRequestFunc: jsonBody` (default for POST):
```json
{
  "feed": { /* feed metadata */ },
  "feedPost": { /* mapped post fields */ },
  "nostrEvent": { /* event being prepared */ }
}
```
- When `composeRequestFunc: queryParams` (default for GET):
  - Query parameters: `feed`, `feedPost`, `nostrEvent` as JSON strings

REST response schema (jsonParse parser):
```json
{
  "result": "success" | "error",
  "nostrEvent": { /* full nostr event to use */ }
}
```

If `result` is not `success` or HTTP is non-2xx, publishing of that post is aborted.

Payload field shapes:
- `feed`: feed metadata, fields include `url`, `pub`, `npub`, `title`, `description`, `link`, `image`.
- `feedPost`: stable struct derived from the RSS/Atom item with fields:
  - `title`, `description`, `link`, `guid`, `published`, `published_unix`, `categories[]`, `enclosures[]`
- `nostrEvent`: standard Nostr event object (pre-signing), fields like `pubkey`, `created_at`, `kind`, `tags`, `content`.

### Example REST endpoint (TypeScript / Express)

```ts
import express from 'express';

const app = express();
app.use(express.json({ limit: '1mb' }));

app.post('/enrich', (req, res) => {
  const { feed, feedPost, nostrEvent } = req.body || {};

  if (!nostrEvent) {
    return res.status(400).json({ result: 'error', error: 'missing nostrEvent' });
  }

  // Example: append metadata to content and add a tag
  const updated = { ...nostrEvent };
  const extra = `\n\n#meta feed=${feed?.url ?? ''} post=${feedPost?.guid ?? ''}`;
  updated.content = (updated.content || '') + extra;

  // Ensure tags array exists
  if (!Array.isArray(updated.tags)) updated.tags = [];
  if (feedPost?.categories?.length) {
    for (const cat of feedPost.categories) {
      updated.tags.push(["t", String(cat)]);
    }
  }

  return res.json({ result: 'success', nostrEvent: updated });
});

app.listen(3000, () => console.log('REST enrich server listening on :3000'));
```

### Create your own hook (Go)

You can either use the REST hook (no-code) or add a custom Go hook.

Steps to add a custom Go hook:

1) Implement the hook interface

```go
// customhook.go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

// MyHook calls your API and can modify the event before signing/publishing
type MyHook struct {
	endpoint string
	token    string
	client   *http.Client
}

func NewMyHook(endpoint, token string) *MyHook {
	return &MyHook{endpoint: endpoint, token: token, client: &http.Client{Timeout: 10 * time.Second}}
}

func (h *MyHook) BeforePublish(ctx context.Context, feed feedStruct, feedPost feedPostStruct, event *nostr.Event) (*nostr.Event, error) {
	payload := map[string]any{"feed": feed, "feedPost": feedPost, "nostrEvent": event}
	body, err := json.Marshal(payload)
	if err != nil { return nil, err }

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.endpoint, bytes.NewReader(body))
	if err != nil { return nil, err }
	req.Header.Set("Content-Type", "application/json")
	if h.token != "" { req.Header.Set("Authorization", "Bearer "+h.token) }

	resp, err := h.client.Do(req)
	if err != nil { return nil, err }
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { return nil, fmt.Errorf("non-2xx: %d", resp.StatusCode) }

	var out struct {
		Result     string        `json:"result"`
		NostrEvent *nostr.Event  `json:"nostrEvent"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil { return nil, err }
	if out.Result != "success" { return nil, errors.New("api returned error result") }
	if out.NostrEvent == nil { return event, nil }
	return out.NostrEvent, nil
}
```

2) Register your hook from YAML

Extend YAML config to include a type for your hook (e.g., `type: myHook`) and handle it in the loader (switch-case) to construct `NewMyHook(...)`. Then use it in `hooks.yaml`:

```yaml
hooks:
  prePostNostrPublish:
    - name: myCustom
      type: myHook
      customEndpoint: https://api.example.com/enrich
      customToken: $MY_API_TOKEN
```

Tips:
- Return an error from `BeforePublish` to stop publishing that post.
- Modify the `event` fields as needed before signing occurs.
- For many use cases, `type: restEnrich` is enough and avoids writing Go code.

## CLI Usage

Add a feed:

    docker exec -it atomstr ./atomstr -a https://my.feed.org/rss

List all feeds:

    docker exec -it atomstr ./atomstr -l


Delete a feed:

    docker exec -it atomstr ./atomstr -d https://my.feed.org/rss

Prune old published posts:

    docker exec -it atomstr ./atomstr -p 30d    # Remove posts older than 30 days
    docker exec -it atomstr ./atomstr -p 7d     # Remove posts older than 7 days  
    docker exec -it atomstr ./atomstr -p 168h   # Remove posts older than 168 hours (7 days)


## About

Questions? Ideas? File bugs and TODOs through the [issue
tracker](https://todo.sr.ht/~psic4t/atomstr) or send an email to
[~psic4t/public-inbox@todo.sr.ht](mailto:~psic4t/public-inbox@todo.sr.ht)
