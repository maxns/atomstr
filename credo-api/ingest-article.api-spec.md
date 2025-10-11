# Article Ingestion API Specification

## Overview

The Article Ingestion API provides a backend-to-backend service for processing articles with automatic AI analysis, interest detection, and assist creation. This endpoint is designed for service-to-service communication and requires backend authentication.

## Endpoint

### POST /api/ingest/article

**Description**: Ingest articles from backend services with automatic AI analysis, interest detection, and assist creation

**Authentication**: Backend key only (service-to-service communication)

**Headers**:
- `Content-Type: application/json`
- `X-Auth-Key: <backend-secret-key>`

## Request Schema

```typescript
{
  "url": string,        // Article URL (required)
  "title"?: string,     // Article title (optional, defaults to empty string)
  "content": string,    // Article content (required, can be empty string)
  "language"?: string   // Article language code (optional, defaults to "en")
}
```

## Response Schema

### Success Response

```typescript
{
  "success": true,
  "articleId"?: string,           // Hash-based article ID (only if article was created)
  "features"?: string[],          // AI-detected features (e.g., ["unbait"])
  "assists"?: {                   // AI analysis results
    "getFeatures"?: {             // Always included - article analysis results
      "features": string[],
      "estimatedReadTimeMinutes": number,
      "topicHashTags": string[], // hashtags, including #
      "clarity": string,
      "languageCode": string,
      "tone": string,
      "length": string,
      "complexity": string,
      "analysisSummaryInUserLanguage": string
    },
    "unbait"?: {                  // Only if clickbait was detected
      "clickbaitScore": number,
      "feature": "unbait",
      "meta": {
        "articleMeta": {
          "title": string,
          "links": any[],
          "images": any[]
        }
      },
      "unbaitAnswer": string,
      "why": string,
      "unbaitTitle": string
    }
  },
  "message": string               // Success message with optional article ID
}
```

### Error Response

```typescript
{
  "success": false,
  "message": string               // Error description
}
```

## Processing Logic

The endpoint follows this processing workflow:

1. **Validation**: Validates required fields (`url`, `content`)
2. **AI Analysis**: Calls `get-article-features` to analyze content and extract hashtags
3. **Interest Detection**: Uses `TopicService.getTopicsOfInterest()` to check if hashtags match configured topics
4. **Article Creation**: Creates article in database only if content matches topics of interest
5. **Feature Processing**: If AI suggests "unbait" feature, executes unbait analysis and creates assist
6. **Database Persistence**: Saves both articles and assists to database for future retrieval

## Topics of Interest

Articles are only created in the database if they contain hashtags related to:
- Bitcoin, cryptocurrency, blockchain
- NOSTR, lightning network
- Other configurable crypto/tech topics

Non-matching content is processed for AI analysis but not persisted.

## HTTP Status Codes

- `200 OK`: Request processed successfully (check `success` field for actual result)
- `400 Bad Request`: Missing required fields or validation error
- `401 Unauthorized`: Invalid backend authentication key
- `500 Internal Server Error`: AI analysis failed or database error

## Examples

### Example 1: Interesting Content (Creates Article)

**Request**:
```bash
curl -X POST http://localhost:7777/api/ingest/article \
  -H "Content-Type: application/json" \
  -H "X-Auth-Key: your-backend-key" \
  -d '{
    "url": "https://example.com/bitcoin-news",
    "title": "You Won'\''t Believe This Bitcoin Discovery!",
    "content": "Bitcoin has seen increased institutional adoption in 2024.",
    "language": "en"
  }'
```

**Response**:
```json
{
  "success": true,
  "articleId": "abc123",
  "features": ["unbait"],
  "assists": {
    "getFeatures": {
      "features": ["unbait"],
      "estimatedReadTimeMinutes": 1,
      "topicHashTags": ["#bitcoin", "#cryptocurrency", "#adoption"],
      "clarity": "high",
      "languageCode": "en",
      "tone": "informative",
      "length": "short",
      "complexity": "low",
      "analysisSummaryInUserLanguage": "Article discusses Bitcoin adoption trends"
    },
    "unbait": {
      "clickbaitScore": 1,
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
  "message": "Article processed successfully (ID: abc123)"
}
```

### Example 2: Non-Interest Content (Skips Article Creation)

**Request**:
```bash
curl -X POST http://localhost:7777/api/ingest/article \
  -H "Content-Type: application/json" \
  -H "X-Auth-Key: your-backend-key" \
  -d '{
    "url": "https://example.com/recipe",
    "title": "Best Chocolate Cake Recipe",
    "content": "This delicious cake recipe has been passed down for generations."
  }'
```

**Response**:
```json
{
  "success": true,
  "features": [],
  "assists": {
    "getFeatures": {
      "features": [],
      "estimatedReadTimeMinutes": 1,
      "topicHashTags": ["#recipe", "#baking", "#cooking"],
      "clarity": "high",
      "languageCode": "en",
      "tone": "friendly",
      "length": "short",
      "complexity": "low",
      "analysisSummaryInUserLanguage": "Recipe for chocolate cake"
    }
  },
  "message": "Article processed successfully"
}
```

### Example 3: Minimal Request (Optional Fields)

**Request**:
```bash
curl -X POST http://localhost:7777/api/ingest/article \
  -H "Content-Type: application/json" \
  -H "X-Auth-Key: your-backend-key" \
  -d '{
    "url": "https://example.com/minimal",
    "content": "Bitcoin price reaches new highs."
  }'
```

**Response**:
```json
{
  "success": true,
  "articleId": "xyz789",
  "features": [],
  "assists": {
    "getFeatures": {
      "features": [],
      "estimatedReadTimeMinutes": 1,
      "topicHashTags": ["#bitcoin", "#price"],
      "clarity": "high",
      "languageCode": "en",
      "tone": "neutral",
      "length": "very-short",
      "complexity": "low",
      "analysisSummaryInUserLanguage": "Brief update on Bitcoin price"
    }
  },
  "message": "Article processed successfully (ID: xyz789)"
}
```

### Example 4: Empty Content (Graceful Handling)

**Request**:
```bash
curl -X POST http://localhost:7777/api/ingest/article \
  -H "Content-Type: application/json" \
  -H "X-Auth-Key: your-backend-key" \
  -d '{
    "url": "https://example.com/empty",
    "content": ""
  }'
```

**Response**:
```json
{
  "success": true,
  "features": [],
  "message": "Article processed successfully (empty content, no analysis performed)"
}
```

## Error Examples

### Missing Required Fields

**Request**:
```bash
curl -X POST http://localhost:7777/api/ingest/article \
  -H "Content-Type: application/json" \
  -H "X-Auth-Key: your-backend-key" \
  -d '{
    "url": "https://example.com/incomplete"
  }'
```

**Response**:
```json
{
  "success": false,
  "message": "Missing required fields: url, content"
}
```

### Invalid Authentication

**Request**:
```bash
curl -X POST http://localhost:7777/api/ingest/article \
  -H "Content-Type: application/json" \
  -H "X-Auth-Key: invalid-key" \
  -d '{
    "url": "https://example.com/test",
    "content": "Test content"
  }'
```

**Response**:
```json
{
  "error": "Invalid backend authentication key",
  "code": "INVALID_AUTH_KEY"
}
```

### Duplicate Content

**Request**: (Same content as previously ingested)

**Response**:
```json
{
  "success": false,
  "message": "Internal server error: Unique constraint failed on the fields: (`id`)"
}
```

### AI Analysis Failure

**Response**:
```json
{
  "success": false,
  "message": "Failed to analyze article features"
}
```

## Integration Notes

### Content Uniqueness
- Articles are identified by a hash of their content
- Duplicate content will be rejected with a unique constraint error
- This is a feature to prevent duplicate article creation

### Interest Detection
- Only articles with "interesting" hashtags are persisted to the database
- All articles receive AI analysis regardless of interest level
- Interest topics are configurable and currently focus on crypto/tech content

### Assist Creation
- `getFeatures` assist is always included in the response (when AI analysis succeeds)
- `unbait` assist is only created when clickbait is detected
- Assists are persisted to the database only for articles that match topics of interest

### Performance Considerations
- AI analysis can take 2-30 seconds depending on content length
- Empty content is handled immediately without AI processing
- Failed AI analysis does not prevent successful response (graceful degradation)

## Testing

A comprehensive test suite is available at:
```bash
node test/integration/ingest/test-ingest-endpoints.cjs
```

The test suite includes:
- API response validation
- Database persistence verification via GraphQL
- Interest detection testing
- Feature processing validation
- Error handling verification
- Automatic cleanup of test data

## Environment Configuration

Required environment variables:
```bash
# Backend authentication
BACKEND_AUTH_KEY=your-secure-backend-key-here

# AI service configuration (inherited from main service)
# Database configuration (inherited from main service)
```

## Rate Limiting

- No specific rate limiting implemented
- Inherits general API rate limiting from main service
- AI processing time provides natural throttling (2-30 seconds per request)

## Monitoring

Key metrics to monitor:
- Request success/failure rates
- AI analysis response times
- Article creation rates (vs. total requests)
- Assist creation rates by type
- Database constraint violations (duplicate content)

## Changelog

### v1.0.0
- Initial implementation with get-features and unbait processing
- Backend-only authentication
- Interest-based article persistence
- Comprehensive test suite

### v1.1.0
- Made `title` and `language` parameters optional
- Added `getFeatures` to response assists
- Removed debug field from response
- Improved error handling for empty content
