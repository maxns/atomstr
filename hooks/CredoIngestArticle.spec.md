# CredoIngestArticle API Specification

## Endpoint
POST /api/ingest/article

## Description
Ingests articles from backend services with automatic AI analysis, interest detection, and assist creation. This hook extracts article content from Nostr events and sends it to the Credo API for processing.

## Request

### Headers
```go
Content-Type: application/json
X-Auth-Key: <backend-secret-key>
```

### Request Body
```go
type CredoIngestArticleRequest struct {
	// Required: Article URL extracted from Nostr event
	URL string `json:"url"`
	// Optional: Article title (defaults to empty string)
	Title string `json:"title,omitempty"`
	// Required: Article content extracted from Nostr event
	Content string `json:"content"`
	// Optional: Article language code (defaults to "en")
	Language string `json:"language,omitempty"`
}
```

### Validation Rules
- `url` must be provided and non-empty
- `content` must be provided (can be empty string)

## Response

### Success Response
```go
type CredoIngestArticleResponse struct {
	Success   bool     `json:"success"`
	Tags      []string `json:"tags,omitempty"` // Root level tags array from API response
	ArticleId string   `json:"articleId,omitempty"`
	Features  []string `json:"features,omitempty"`
	Assists   *struct {
		GetFeatures *struct {
			Features                      []string `json:"features"`
			EstimatedReadTimeMinutes        float64  `json:"estimatedReadTimeMinutes"`
			TopicHashTags                   []string `json:"topicHashTags"` // hashtags, including #
			Clarity                         string   `json:"clarity"`
			LanguageCode                    string   `json:"languageCode"`
			Tone                           string   `json:"tone"`
			Length                         string   `json:"length"`
			Complexity                     string   `json:"complexity"`
			AnalysisSummaryInUserLanguage  string   `json:"analysisSummaryInUserLanguage"`
		} `json:"getFeatures,omitempty"`
		
		// Only if clickbait was detected
		Unbait *struct {
			ClickbaitScore float64 `json:"clickbaitScore"`
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
	Message string `json:"message,omitempty"`
	TimeMs  int64  `json:"timeMs,omitempty"`
}
```

### Error Response
```go
type ErrorResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
	TimeMs  int64  `json:"timeMs,omitempty"`
}
```

## Example

### Request
```json
{
	"url": "https://example.com/bitcoin-news",
	"title": "You Won't Believe This Bitcoin Discovery!",
	"content": "Bitcoin has seen increased institutional adoption in 2024.",
	"language": "en"
}
```

### Success Response
```json
{
	"success": true,
	"articleId": "abc123",
	"features": ["unbait"],
	"tags": ["#bitcoin", "#cryptocurrency", "#adoption"],
	"assists": {
		"getFeatures": {
			"features": ["unbait"],
			"estimatedReadTimeMinutes": 0.5,
			"topicHashTags": ["#bitcoin", "#cryptocurrency"],
			"clarity": "high",
			"languageCode": "en",
			"tone": "informative",
			"length": "short",
			"complexity": "low",
			"analysisSummaryInUserLanguage": "Article discusses Bitcoin adoption trends"
		},
		"unbait": {
			"clickbaitScore": 0.1,
			"feature": "unbait",
			"meta": {
				"articleMeta": {
					"title": "You Won't Believe This Bitcoin Discovery!",
					"links": [],
					"images": []
				}
			},
			"unbaitAnswer": "Bitcoin has seen increased institutional adoption in 2024.",
			"why": "The title uses sensational language to create intrigue",
			"unbaitTitle": "Bitcoin Sees Increased Institutional Adoption in 2024"
		}
	},
	"message": "Article processed successfully (ID: abc123)",
	"timeMs": 1648176000000
}
```

### Error Response
```json
{
	"success": false,
	"error": "Missing required fields: url, content",
	"message": "Invalid request parameters",
	"timeMs": 1648176000000
}
```

## Hook Integration

### Configuration
This hook is implemented as a `customHook` and registers itself automatically. Configure it in `hooks.yaml`:

```yaml
hooks:
  prePostNostrPublish:
    - name: ingestArticleToCredo
      type: customHook
      hookName: credoIngestArticle
      config:
        url: https://credo-api.example.com/api/ingest/article
        headers:
          X-Auth-Key: "$CREDO_BACKEND_AUTH_KEY"
```

### Content Extraction
The hook extracts article information from Nostr events using the following logic:
1. **URL Extraction**: Looks for URLs in the event content using regex patterns
2. **Title Extraction**: Uses the first line of content or derives from URL
3. **Content Processing**: Uses the full event content as article content
4. **Language Detection**: Defaults to "en" but can be configured

### Event Processing
- The hook processes Nostr events before they are published
- It extracts article URLs and content from the event
- Sends the extracted data to the Credo API for analysis
- Adds topic hashtags from the API response as "t" tags to the event
- Strips "#" prefixes from hashtags before adding them as tags
- Embeds complete AI analysis results as JSON-encoded "alt" tags:
  - `aiAssist:get-features` - always included when analysis succeeds
  - `aiAssist:unbait` - only included when clickbait is detected
- Replaces existing "t" tags with the new `tags` array from the API response

### Tag Processing Details

#### Topic Tags (from root `tags` array)
The root-level `tags` array from the response is used to replace all existing "t" tags on the Nostr event.
- Format: `["t", "<tag_name>"]`
- The "#" prefix is automatically stripped from tags
- Example: `#bitcoin` becomes `["t", "bitcoin"]`

#### AI Assist Tags (from analysis results)
AI analysis results are embedded as complete JSON objects in "alt" tags:

1. **Get-Features Assist**: `["alt", "aiAssist:get-features", "<json>"]`
   - Contains the complete `getFeatures` analysis result as JSON string
   - Always included when AI analysis succeeds
   - Includes: features, readTime, topicHashTags, clarity, tone, etc.

2. **Unbait Assist**: `["alt", "aiAssist:unbait", "<json>"]`
   - Contains the complete `unbait` analysis result as JSON string
   - Only included when clickbait is detected
   - Includes: clickbaitScore, unbaitAnswer, unbaitTitle, why, meta, etc.

#### Complete Tag Example
For a clickbait article about Bitcoin, the event might include:
```json
[
  ["t", "bitcoin"],
  ["t", "cryptocurrency"], 
  ["t", "adoption"],
  ["alt", "aiAssist:get-features", "{\"features\":[\"unbait\"],\"estimatedReadTimeMinutes\":1,\"topicHashTags\":[\"#bitcoin\",\"#cryptocurrency\"],\"clarity\":\"high\",\"languageCode\":\"en\",\"tone\":\"informative\",\"length\":\"short\",\"complexity\":\"low\",\"analysisSummaryInUserLanguage\":\"Article discusses Bitcoin adoption trends\"}"],
  ["alt", "aiAssist:unbait", "{\"clickbaitScore\":1,\"feature\":\"unbait\",\"unbaitAnswer\":\"Bitcoin has seen increased institutional adoption in 2024.\",\"unbaitTitle\":\"Bitcoin Sees Increased Institutional Adoption in 2024\",\"why\":\"The title uses sensational language to create intrigue\"}"]
]
```

### Error Handling
- Network failures are logged but do not prevent event publishing
- API errors are logged with detailed error messages
- Invalid responses are handled gracefully
- Timeout protection prevents hanging requests

## Notes
1. This hook performs both article ingestion and event enrichment with topic tags
2. The hook operates during event publishing and can modify the event by adding tags
3. Failed API calls do not prevent the Nostr event from being published
4. The hook supports both URL extraction from content and explicit URL parameters
5. Authentication is handled via backend keys in headers
6. The response includes comprehensive AI analysis results when successful
7. Articles are only persisted if they match configured topics of interest
8. **Tag Replacement**: The root `tags` array from the API response replaces all existing "t" tags on the event.
9. **AI Assist Tags**: Complete analysis results are embedded as JSON in "alt" tags:
   - `aiAssist:get-features` - complete getFeatures analysis (always included)
   - `aiAssist:unbait` - complete unbait analysis (when clickbait detected)
10. JSON embedding preserves all analysis data without creating custom schemas
11. Tag deduplication prevents adding tags that already exist on the event
12. Both "t" and "alt" tag types follow standard Nostr tag format: `