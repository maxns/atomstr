# Credo Web Node Backend - API Specification

This document provides comprehensive documentation for all API endpoints in the Credo Web Node Backend service.

## Table of Contents

1. [Quick Reference](#quick-reference)
2. [Base URL](#base-url)
3. [Authentication](#authentication)
4. [CORS Configuration](#cors-configuration)
5. [Response Format](#response-format)
6. [Error Codes](#error-codes)
7. [Endpoints](#endpoints)
   - [System Endpoints](#system-endpoints)
   - [User Authentication Endpoints](#user-authentication-endpoints)
   - [Tag Generation Endpoints](#tag-generation-endpoints)
   - [Article Assist Endpoints](#article-assist-endpoints)
8. [Error Responses](#error-responses)
9. [Rate Limiting & Timeouts](#rate-limiting--timeouts)
10. [Environment Configuration](#environment-configuration)
11. [Testing](#testing)

## Quick Reference

### System Endpoints Summary

| Endpoint | Method | Purpose | Auth Required | Response |
|----------|--------|---------|---------------|----------|
| `/` | GET | Welcome message | None | Service identification |
| `/health` | GET | Health check | None | Status and timestamp |
| `/api/status` | GET | Detailed status | None | Status with AI client info |

### User Authentication Endpoints Summary

| Endpoint | Method | Purpose | Auth Required | Key Parameters |
|----------|--------|---------|---------------|----------------|
| `/user/init-session` | POST | Initialize user session | Headers Only | `X-Session-ID`, `X-User-ID` headers |
| `/user/ping-session` | POST | Keep session alive | None | `sessionId`, `userId` |
| `/user/login` | POST | Login with various methods | None | `method`, `deviceId`, `appleToken` |
| `/user/setup` | POST | Create new user account | None | `user` (UserSetupInput) |
| `/user/logout` | POST | Logout and invalidate session | None | `sessionId`, `userId` |

### Business API Endpoints Summary

| Endpoint | Method | Purpose | Auth Required | Key Parameters |
|----------|--------|---------|---------------|----------------|
| `/api/tags-for-note` | POST | Generate AI tags for notes | Backend OR User | `byEvent.event.content` or `byEventId` |
| `/api/suggest-tags-v2` | POST | Advanced tag suggestions | Backend OR User | `note`, `existingTags`, `searchText` |
| `/api/suggest-tags` | POST | Legacy tag suggestions | Backend Only | Same as suggest-tags-v2 |
| `/api/unbait-article` | POST | Remove clickbait from articles | Backend OR User | `url`, `sensitivity`, `brevity` |
| `/api/get-article-features` | POST | Analyze article features | Backend OR User | `url`, `language` |
| `/api/summarize-article` | POST | Generate article summaries | Backend OR User | `url`, `language`, `interestTopics` |

### Authentication Quick Reference

| Method | Header | Value | Use Case |
|--------|--------|-------|----------|
| Backend Auth | `X-Auth-Key` | Backend secret key | Service-to-service |
| User Session | `X-Session-ID` + `X-User-ID` | Session token + User ID | Client apps |
| User Bearer | `Authorization` + `X-User-ID` | `Bearer <token>` + User ID | Client apps |

## Base URL

- **Development**: `http://localhost:7777`
- **Production**: Configured via environment variables

## Authentication

The API supports two authentication methods:

### 1. Backend-to-Backend Authentication
Used for service-to-service communication.

**Header**: `X-Auth-Key`
**Value**: Shared secret configured via `BACKEND_AUTH_KEY` environment variable

```bash
curl -H "X-Auth-Key: your-backend-key"
```

### 2. User Session Authentication
Used for client applications (web/mobile).

**Method 1 - Session ID Header**:
```bash
curl -H "X-Session-ID: user-session-token" -H "X-User-ID: user-id"
```

**Method 2 - Bearer Token**:
```bash
curl -H "Authorization: Bearer user-session-token" -H "X-User-ID: user-id"
```

## CORS Configuration

The API accepts requests from configured origins with the following headers:
- `Content-Type`
- `Authorization`
- `X-Requested-With`
- `X-Auth-Key` (Backend authentication)
- `X-Session-ID` (User session authentication)
- `X-User-ID` (User ID for session validation)

## Response Format

All API responses follow this standard format:

```typescript
{
  "success": boolean,
  "message"?: string,
  "result": any | null,
  "error"?: string
}
```

## Error Codes

- `INVALID_AUTH_KEY` - Invalid backend authentication key
- `INVALID_SESSION` - Invalid or expired user session
- `AUTH_REQUIRED` - Authentication required but not provided
- `SESSION_VALIDATION_ERROR` - Session validation failed
- `INVALID_REQUEST` - Request validation failed
- `MISSING_PARAMETERS` - Required parameters missing
- `MISSING_METHOD` - Login method not specified
- `MISSING_DEVICE_ID` - Device ID required for device login
- `MISSING_APPLE_TOKEN` - Apple token required for Apple login
- `MISSING_USER_DATA` - User data required for setup
- `INVALID_CREDENTIALS` - Login credentials invalid
- `INVALID_METHOD` - Login method not supported
- `NOT_IMPLEMENTED` - Feature not yet implemented

---

## Endpoints

### System Endpoints

#### GET /
**Description**: Welcome message and service identification

**Authentication**: None required

**Response**:
```json
{
  "message": "Welcome to @credo/web-node-backend API"
}
```

#### GET /health
**Description**: Health check endpoint for monitoring

**Authentication**: None required

**Response**:
```json
{
  "status": "OK",
  "timestamp": "2024-01-01T00:00:00.000Z"
}
```

#### GET /api/status
**Description**: Detailed status including AI client information

**Authentication**: None required

**Response**:
```json
{
  "status": "ok",
  "timestamp": "2024-01-01T00:00:00.000Z",
  "aiClientStatus": {
    // AI client status information
  }
}
```

---

### User Authentication Endpoints

#### POST /user/init-session
**Description**: Initialize a user session from existing session headers

**Authentication**: Session headers required (`X-Session-ID` and `X-User-ID`)

**Headers**:
- `X-Session-ID`: User session token
- `X-User-ID`: User ID for validation

**Request Body**: Empty (authentication via headers)

**Response**:
```json
{
  "success": true,
  "result": {
    "user": {
      "id": "user-id",
      "username": "username",
      "email": "user@example.com",
      "status": "ACTIVE",
      "createdAt": "2024-01-01T00:00:00.000Z",
      "updatedAt": "2024-01-01T00:00:00.000Z",
      "activeProfile": {
        "id": "profile-id",
        "name": "Display Name",
        "displayName": "Display Name",
        "bio": "User bio",
        "profileImageURL": "https://...",
        "coverImageURL": "https://...",
        "visibility": "PUBLIC",
        "status": "ACTIVE"
      },
      "settings": {
        "language": "en",
        "timezone": "UTC",
        "theme": "light"
      }
    }
  }
}
```

**Error Responses**:
- `401 AUTH_REQUIRED`: Missing session headers
- `401 INVALID_SESSION`: Invalid or expired session

**Example**:
```bash
curl -X POST http://localhost:7777/user/init-session \
  -H "X-Session-ID: session-token-here" \
  -H "X-User-ID: user-id-here"
```

#### POST /user/ping-session
**Description**: Keep session alive by updating last active time

**Authentication**: None (validates session in request body)

**Request Body**:
```typescript
{
  "sessionId": string,
  "userId": string
}
```

**Response**:
```json
{
  "success": true,
  "result": {
    "success": true,
    "expiresAt": "2024-01-02T00:00:00.000Z"
  }
}
```

**Error Responses**:
- `400 MISSING_PARAMETERS`: Missing sessionId or userId
- `401 INVALID_SESSION`: Invalid session

**Example**:
```bash
curl -X POST http://localhost:7777/user/ping-session \
  -H "Content-Type: application/json" \
  -d '{
    "sessionId": "session-id",
    "userId": "user-id"
  }'
```

#### POST /user/login
**Description**: Login user with various authentication methods

**Authentication**: None (creates new session)

**Request Body**:
```typescript
{
  "method": "device" | "apple" | "username",
  "deviceId"?: string,      // Required for device method
  "appleToken"?: string,    // Required for apple method
  "username"?: string,      // Future: for username method
  "passcode"?: string       // Future: for username method
}
```

**Response**:
```json
{
  "success": true,
  "result": {
    "user": {
      "id": "user-id",
      "username": "username",
      "email": "user@example.com",
      "status": "ACTIVE",
      "activeProfile": { /* UserProfile object */ },
      "settings": { /* UserSettings object */ }
    },
    "session": {
      "id": "session-id",
      "userId": "user-id",
      "status": "ACTIVE",
      "expiresAt": "2024-01-02T00:00:00.000Z",
      "createdAt": "2024-01-01T00:00:00.000Z"
    }
  }
}
```

**Error Responses**:
- `400 MISSING_METHOD`: Login method not specified
- `400 MISSING_DEVICE_ID`: Device ID required for device login
- `400 MISSING_APPLE_TOKEN`: Apple token required for Apple login
- `401 INVALID_CREDENTIALS`: Login credentials invalid
- `400 INVALID_METHOD`: Login method not supported
- `501 NOT_IMPLEMENTED`: Apple/username login not yet implemented

**Example - Device Login**:
```bash
curl -X POST http://localhost:7777/user/login \
  -H "Content-Type: application/json" \
  -d '{
    "method": "device",
    "deviceId": "unique-device-identifier"
  }'
```

**Example - Apple Login** (Future):
```bash
curl -X POST http://localhost:7777/user/login \
  -H "Content-Type: application/json" \
  -d '{
    "method": "apple",
    "appleToken": "apple-jwt-token"
  }'
```

#### POST /user/setup
**Description**: Create a new user account with initial profile and login

**Authentication**: None (creates new user and session)

**Request Body**:
```typescript
{
  "user": {
    "username": string,
    "email"?: string,
    "settings"?: {
      "language"?: string,
      "timezone"?: string,
      "theme"?: string,
      "notificationSettings"?: Array<{
        "source": string,
        "enabled": boolean
      }>
    },
    "activeProfile": {
      "name": string,
      "displayName"?: string,
      "bio"?: string,
      "profileImageURL"?: string,
      "coverImageURL"?: string,
      "socials"?: Array<{
        "platform": string,
        "handle": string,
        "displayName"?: string,
        "url"?: string
      }>,
      "deviceTokens"?: Array<{
        "token": string,
        "platform": string,
        "lastReachableAt"?: string
      }>,
      "interestTopicTags"?: string[]
    },
    "initialLogin"?: Array<{
      "type": "DEVICE" | "SOCIAL" | "EMAIL" | "OAUTH" | "USERNAME",
      "provider": string,
      "identifier": string,
      "keyHash"?: string,
      "expiresAt"?: string,
      "meta"?: Array<{
        "key": string,
        "value": string
      }>
    }>
  }
}
```

**Response**:
```json
{
  "success": true,
  "result": {
    "user": {
      "id": "new-user-id",
      "username": "new-username",
      "email": "user@example.com",
      "status": "ACTIVE",
      "activeProfile": { /* Created profile */ },
      "settings": { /* User settings */ }
    },
    "session": {
      "id": "new-session-id",
      "userId": "new-user-id",
      "status": "ACTIVE",
      "expiresAt": "2024-01-02T00:00:00.000Z"
    }
  }
}
```

**Error Responses**:
- `400 MISSING_USER_DATA`: User data required

**Example**:
```bash
curl -X POST http://localhost:7777/user/setup \
  -H "Content-Type: application/json" \
  -d '{
    "user": {
      "username": "newuser123",
      "email": "newuser@example.com",
      "activeProfile": {
        "name": "New User",
        "displayName": "New User",
        "bio": "Hello, I am new here!"
      },
      "initialLogin": [{
        "type": "DEVICE",
        "provider": "device",
        "identifier": "device-unique-id"
      }]
    }
  }'
```

#### POST /user/logout
**Description**: Logout user and invalidate session

**Authentication**: None (validates session in request body)

**Request Body**:
```typescript
{
  "sessionId": string,
  "userId": string
}
```

**Response**:
```json
{
  "success": true,
  "result": {
    "success": true
  }
}
```

**Error Responses**:
- `400 MISSING_PARAMETERS`: Missing sessionId or userId

**Example**:
```bash
curl -X POST http://localhost:7777/user/logout \
  -H "Content-Type: application/json" \
  -d '{
    "sessionId": "session-to-invalidate",
    "userId": "user-id"
  }'
```

---

### Tag Generation Endpoints

#### POST /api/tags-for-note
**Description**: Generate tags for a note using AI

**Authentication**: Backend key OR user session

**Request Body**:
```typescript
{
  "params": {
    "byEvent"?: {
      "event": {
        "id": string,
        "pubkey": string,
        "content": string,
        "created_at": number,
        "kind": number,
        "tags": string[][],
        "sig": string
      },
      "linkedNotes"?: any[]
    },
    "byEventId"?: {
      "id": string,
      "relayUrls"?: string[]
    }
  },
  "options": {
    "maxTags"?: number,
    "retry"?: false | number | string
  }
}
```

**Response**:
```json
{
  "success": true,
  "result": {
    "tags": ["tag1", "tag2", "tag3"]
  }
}
```

**Example**:
```bash
curl -X POST http://localhost:7777/api/tags-for-note \
  -H "Content-Type: application/json" \
  -H "X-Auth-Key: your-backend-key" \
  -d '{
    "params": {
      "byEvent": {
        "event": {
          "id": "sample-id",
          "pubkey": "sample-pubkey",
          "content": "This is a sample note about AI and technology trends",
          "created_at": 1640995200,
          "kind": 1,
          "tags": [],
          "sig": "sample-signature"
        }
      }
    },
    "options": {}
  }'
```

#### POST /api/suggest-tags-v2
**Description**: Advanced tag suggestions with additional parameters

**Authentication**: Backend key OR user session

**Request Body**:
```typescript
{
  "params": {
    "byEvent"?: {
      "event": NostrEvent,
      "linkedNotes"?: any[]
    },
    "byEventId"?: {
      "id": string,
      "relayUrls"?: string[]
    },
    "note"?: string,
    "existingTags"?: string[],
    "searchText"?: string
  },
  "options": {
    "maxTags"?: number,
    "retry"?: false | number | string
  }
}
```

**Response**:
```json
{
  "success": true,
  "result": {
    "tags": ["suggested-tag1", "suggested-tag2"]
  }
}
```

#### POST /api/suggest-tags
**Description**: Legacy tag suggestion endpoint (backend only)

**Authentication**: Backend key only

**Request Body**: Same as `/api/suggest-tags-v2`

---

### Article Assist Endpoints

#### POST /api/unbait-article
**Description**: Remove clickbait elements from article titles and content

**Authentication**: Backend key OR user session

**Request Body**:
```typescript
{
  "params": {
    "feature": "unbait",
    "url": string,
    "title"?: string,
    "content"?: string,
    "language"?: string,
    "sensitivity": number, // 0-1 (0 = least sensitive, 1 = most sensitive)
    "brevity": number // 0-1 (0 = least brief, 1 = most brief)
  },
  "options": {}
}
```

**Response**:
```json
{
  "success": true,
  "result": {
    "unbaitAnswer": "Factual version of the content",
    "unbaitTitle": "Non-clickbait title",
    "meta": {
      "articleMeta": {
        // Article metadata
      }
    }
  }
}
```

**Example**:
```bash
curl -X POST http://localhost:7777/api/unbait-article \
  -H "Content-Type: application/json" \
  -H "X-Auth-Key: your-backend-key" \
  -d '{
    "params": {
      "feature": "unbait",
      "url": "https://example.com/article",
      "title": "You Won'\''t Believe What Happened Next!",
      "content": "This shocking discovery will change everything...",
      "language": "english",
      "sensitivity": 0.5,
      "brevity": 0.7
    },
    "options": {}
  }'
```

#### POST /api/get-article-features
**Description**: Extract and analyze article features

**Authentication**: Backend key OR user session

**Request Body**:
```typescript
{
  "params": {
    "feature": "get-features",
    "url": string,
    "title"?: string,
    "content"?: string,
    "language"?: string
  },
  "options": {}
}
```

**Response**:
```json
{
  "success": true,
  "result": {
    "features": ["unbait", "summarize"],
    "estimatedReadTimeMinutes": 5,
    "topicHashTags": ["#technology", "#ai"],
    "clarity": "high",
    "languageCode": "en",
    "tone": "informative",
    "length": "medium",
    "complexity": "medium",
    "analysisSummaryInUserLanguage": "Brief analysis summary"
  }
}
```

#### POST /api/summarize-article
**Description**: Generate article summary with key insights

**Authentication**: Backend key OR user session

**Request Body**:
```typescript
{
  "params": {
    "feature": "summarize",
    "url": string,
    "title"?: string,
    "content"?: string,
    "language": string,
    "interestTopics": string[],
    "includeInResult"?: string[] // Filter specific result fields
  },
  "options": {}
}
```

**Response**:
```json
{
  "success": true,
  "result": {
    "shortSummary": "Brief summary of the article",
    "keyTakeaways": [
      "Key point 1",
      "Key point 2"
    ],
    "outline": [
      {
        "title": "Section 1",
        "summary": "Summary of section 1"
      }
    ],
    "articleType": "news",
    "interestMatch": {
      "score": "high",
      "why": "Matches user's interest in technology topics"
    }
  }
}
```

**Example**:
```bash
curl -X POST http://localhost:7777/api/summarize-article \
  -H "Content-Type: application/json" \
  -H "X-Auth-Key: your-backend-key" \
  -d '{
    "params": {
      "feature": "summarize",
      "url": "https://example.com/tech-article",
      "language": "english",
      "interestTopics": ["artificial intelligence", "technology", "innovation"]
    },
    "options": {}
  }'
```

---

## Error Responses

### Authentication Errors

**401 Unauthorized - Invalid Backend Key**:
```json
{
  "error": "Invalid backend authentication key",
  "code": "INVALID_AUTH_KEY"
}
```

**401 Unauthorized - Invalid Session**:
```json
{
  "error": "Invalid or expired session: [details]",
  "code": "INVALID_SESSION",
  "details": "Session validation error details"
}
```

**401 Unauthorized - Missing Authentication**:
```json
{
  "error": "Authentication required. Provide either X-Auth-Key for backend calls or X-Session-ID/Authorization Bearer for user requests",
  "code": "AUTH_REQUIRED"
}
```

### Request Errors

**400 Bad Request - Invalid Request**:
```json
{
  "error": "Either byId or byEvent(note, linkedNotes) must be provided",
  "code": "INVALID_REQUEST"
}
```

**500 Internal Server Error**:
```json
{
  "message": "Internal Server Error",
  "error": "Detailed error message (development only)"
}
```

---

## Rate Limiting & Timeouts

- **Session Validation Timeout**: 10 seconds
- **Session Cache TTL**: Configurable (default varies by environment)
- **Request Timeout**: 30 seconds (configurable per endpoint)
- **Max Retries**: 3 (configurable per endpoint)

---

## Environment Configuration

Key environment variables:

```bash
# Server Configuration
PORT=7777
HOST=localhost

# HTTPS Configuration (optional)
HTTPS=true
HTTPS_PORT=8443

# Authentication
BACKEND_AUTH_KEY=your-secure-backend-key-here

# CORS Configuration
CORS_ORIGINS=http://localhost:3000,https://your-app.com
```

---

## Testing

Test scripts are available in `/test/integration/` and `/run/scripts/test/`:
- `test-tag-endpoint.sh` - Tests tag generation endpoints  
- `test-article-endpoints.sh` - Tests article assist endpoints

For article ingestion testing, see the separate [Article Ingestion API Specification](./ingest-article.api-spec.md).

Example usage:
```bash
# Test all article assist endpoints
./test-article-endpoints.sh

# Test with custom URL
./test-article-endpoints.sh "https://example.com/article"

# Test with specific language
./test-article-endpoints.sh --language spanish
```
