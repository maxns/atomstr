# SuggestTags API Specification

## Endpoint
POST /api/suggest-tags

## Description
Suggests tags based on provided content, existing tags, search text, or Nostr events.

## Request

### Headers
```go
Content-Type: application/json
```

### Request Body
```go
type SuggestTagsRequest struct {
    Params struct {
        // Optional: Text content to generate tags for
        Note string `json:"note,omitempty"`
        
        // Optional: List of existing tags to consider
        ExistingTags []string `json:"existingTags,omitempty"`
        
        // Optional: Search text to influence tag suggestions
        SearchText string `json:"searchText,omitempty"`
        
        // Optional: Nostr event details
        ByEvent *struct {
            Event struct {
                Content string `json:"content"`
                // Add other NostrEvent fields if needed
            } `json:"event"`
            LinkedEvents []interface{} `json:"linkedEvents,omitempty"`
        } `json:"byEvent,omitempty"`
        
        // Optional: Reference to Nostr event by ID
        ByEventId *struct {
            Id string `json:"id"`
            RelayUrls []string `json:"relayUrls,omitempty"`
        } `json:"byEventId,omitempty"`
    } `json:"params"`
    
    Options struct {
        // Optional: Maximum number of tags to return
        MaxTags int `json:"maxTags,omitempty"`
        
        // Optional: Retry configuration
        // Format: false, number, or "number|string;different-model|same-model"
        Retry interface{} `json:"retry,omitempty"`
    } `json:"options"`
    
    // Optional: Additional hints for the tag suggestion
    Hints []string `json:"hints,omitempty"`
    
    // Optional: Timestamp in milliseconds
    TimeMs int64 `json:"timeMs,omitempty"`
}
```

### Validation Rules
- At least one of the following must be provided:
  - `params.note`
  - `params.existingTags` (non-empty array with at least one non-empty tag)
  - `params.searchText`
  - `params.byEvent` with valid event content
  - `params.byEventId`

## Response

### Success Response
```go
type SuggestTagsResponse struct {
    Success bool `json:"success"`
    Result *struct {
        Tags []string `json:"tags"`
    } `json:"result"`
    Message string `json:"message,omitempty"`
    TimeMs int64 `json:"timeMs,omitempty"`
}
```

### Error Response
```go
type ErrorResponse struct {
    Success bool `json:"success"`
    Error string `json:"error"`
    Message string `json:"message,omitempty"`
    TimeMs int64 `json:"timeMs,omitempty"`
}
```

## Example

### Request
```json
{
    "params": {
        "note": "Bitcoin is a decentralized digital currency",
        "existingTags": ["crypto"],
        "searchText": "bitcoin cryptocurrency"
    },
    "options": {
        "maxTags": 5
    }
}
```

### Success Response
```json
{
    "success": true,
    "result": {
        "tags": ["bitcoin", "crypto", "cryptocurrency", "blockchain", "btc"]
    },
    "timeMs": 1648176000000
}
```

### Error Response
```json
{
    "success": false,
    "error": "Must provide at least one of: note, existingTags, searchText or byEvent/byEventId",
    "message": "No valid parameters provided",
    "timeMs": 1648176000000
}
```

## Notes
1. The endpoint supports multiple input methods (note text, existing tags, search text, or Nostr events)
2. All request parameters are optional, but at least one valid input must be provided
3. The `byEventId` feature is not currently implemented in the reference implementation
4. The response will always include a `success` boolean indicating whether the operation succeeded
5. The `timeMs` field is optional in both request and response