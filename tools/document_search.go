package tools

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// DocumentSearchTool - A test tool with complex nested filters
type DocumentSearchTool struct{}

func (t *DocumentSearchTool) Name() string {
	return "document_search"
}

func (t *DocumentSearchTool) DisplayName() string {
	return "Search Documents"
}

func (t *DocumentSearchTool) Description() string {
	return "Search documents with complex filters including nested conditions, date ranges, and metadata matching. Useful for testing complex tool argument handling."
}

func (t *DocumentSearchTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Full-text search query",
			},
			"filters": map[string]interface{}{
				"type":        "object",
				"description": "Complex filter conditions",
				"properties": map[string]interface{}{
					"status": map[string]interface{}{
						"type":        "array",
						"description": "Filter by document status (multiple allowed)",
						"items": map[string]interface{}{
							"type": "string",
							"enum": []string{"draft", "published", "archived", "deleted"},
						},
					},
					"date_range": map[string]interface{}{
						"type":        "object",
						"description": "Filter by date range",
						"properties": map[string]interface{}{
							"field": map[string]interface{}{
								"type":        "string",
								"description": "Date field to filter on",
								"enum":        []string{"created_at", "updated_at", "published_at"},
							},
							"start": map[string]interface{}{
								"type":        "string",
								"description": "Start date (ISO 8601 format)",
							},
							"end": map[string]interface{}{
								"type":        "string",
								"description": "End date (ISO 8601 format)",
							},
						},
					},
					"metadata": map[string]interface{}{
						"type":        "array",
						"description": "Filter by metadata key-value pairs",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"key": map[string]interface{}{
									"type":        "string",
									"description": "Metadata key",
								},
								"operator": map[string]interface{}{
									"type":        "string",
									"description": "Comparison operator",
									"enum":        []string{"equals", "not_equals", "contains", "starts_with", "ends_with", "gt", "gte", "lt", "lte"},
								},
								"value": map[string]interface{}{
									"type":        "string",
									"description": "Value to compare against (will be parsed as appropriate type)",
								},
							},
							"required": []string{"key", "operator", "value"},
						},
					},
					"tags": map[string]interface{}{
						"type":        "object",
						"description": "Filter by tags with AND/OR logic",
						"properties": map[string]interface{}{
							"match": map[string]interface{}{
								"type":        "string",
								"description": "How to match tags",
								"enum":        []string{"all", "any", "none"},
							},
							"values": map[string]interface{}{
								"type":        "array",
								"description": "Tag values to match",
								"items": map[string]interface{}{
									"type": "string",
								},
							},
						},
					},
					"author": map[string]interface{}{
						"type":        "object",
						"description": "Filter by author",
						"properties": map[string]interface{}{
							"id": map[string]interface{}{
								"type":        "string",
								"description": "Author ID",
							},
							"name": map[string]interface{}{
								"type":        "string",
								"description": "Author name (partial match)",
							},
							"email": map[string]interface{}{
								"type":        "string",
								"description": "Author email",
							},
						},
					},
				},
			},
			"sort": map[string]interface{}{
				"type":        "array",
				"description": "Sort order (multiple fields supported)",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"field": map[string]interface{}{
							"type":        "string",
							"description": "Field to sort by",
						},
						"direction": map[string]interface{}{
							"type":        "string",
							"enum":        []string{"asc", "desc"},
							"description": "Sort direction",
						},
					},
					"required": []string{"field"},
				},
			},
			"pagination": map[string]interface{}{
				"type":        "object",
				"description": "Pagination options",
				"properties": map[string]interface{}{
					"page": map[string]interface{}{
						"type":        "integer",
						"description": "Page number (1-based)",
						"minimum":     1,
					},
					"per_page": map[string]interface{}{
						"type":        "integer",
						"description": "Results per page",
						"minimum":     1,
						"maximum":     100,
					},
				},
			},
			"include": map[string]interface{}{
				"type":        "array",
				"description": "Related data to include in response",
				"items": map[string]interface{}{
					"type": "string",
					"enum": []string{"author", "comments", "attachments", "versions", "metadata"},
				},
			},
		},
		"required": []string{"query"},
	}
}

// mockDocument represents a document in our mock database
type mockDocument struct {
	ID          string
	Title       string
	Content     string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	PublishedAt *time.Time
	AuthorID    string
	AuthorName  string
	AuthorEmail string
	Tags        []string
	WordCount   int
	Language    string
	Category    string
}

// getMockDocuments returns a set of mock documents for testing
func getMockDocuments() []mockDocument {
	now := time.Now()
	publishedTime := now.Add(-5 * 24 * time.Hour)

	return []mockDocument{
		{
			ID:          "doc_001",
			Title:       "API Integration Guide",
			Content:     "Complete guide to integrating with our REST API endpoints.",
			Status:      "published",
			CreatedAt:   now.Add(-30 * 24 * time.Hour),
			UpdatedAt:   now.Add(-2 * 24 * time.Hour),
			PublishedAt: &publishedTime,
			AuthorID:    "user_123",
			AuthorName:  "John Smith",
			AuthorEmail: "john@example.com",
			Tags:        []string{"api", "integration", "backend"},
			WordCount:   2500,
			Language:    "en",
			Category:    "documentation",
		},
		{
			ID:          "doc_002",
			Title:       "Frontend Components Draft",
			Content:     "Work in progress documentation for React components.",
			Status:      "draft",
			CreatedAt:   now.Add(-7 * 24 * time.Hour),
			UpdatedAt:   now.Add(-1 * 24 * time.Hour),
			PublishedAt: nil,
			AuthorID:    "user_456",
			AuthorName:  "Jane Doe",
			AuthorEmail: "jane@example.com",
			Tags:        []string{"frontend", "react", "components"},
			WordCount:   800,
			Language:    "en",
			Category:    "notes",
		},
		{
			ID:          "doc_003",
			Title:       "Database Schema Reference",
			Content:     "Complete database schema with all tables and relationships.",
			Status:      "published",
			CreatedAt:   now.Add(-60 * 24 * time.Hour),
			UpdatedAt:   now.Add(-10 * 24 * time.Hour),
			PublishedAt: &publishedTime,
			AuthorID:    "user_123",
			AuthorName:  "John Smith",
			AuthorEmail: "john@example.com",
			Tags:        []string{"database", "schema", "backend"},
			WordCount:   3200,
			Language:    "en",
			Category:    "documentation",
		},
		{
			ID:          "doc_004",
			Title:       "Archived Legacy API Docs",
			Content:     "Documentation for deprecated v1 API endpoints.",
			Status:      "archived",
			CreatedAt:   now.Add(-365 * 24 * time.Hour),
			UpdatedAt:   now.Add(-180 * 24 * time.Hour),
			PublishedAt: &publishedTime,
			AuthorID:    "user_789",
			AuthorName:  "Bob Wilson",
			AuthorEmail: "bob@example.com",
			Tags:        []string{"api", "legacy", "deprecated"},
			WordCount:   1200,
			Language:    "en",
			Category:    "archive",
		},
		{
			ID:          "doc_005",
			Title:       "Security Best Practices",
			Content:     "Guidelines for secure API development and authentication.",
			Status:      "published",
			CreatedAt:   now.Add(-14 * 24 * time.Hour),
			UpdatedAt:   now.Add(-3 * 24 * time.Hour),
			PublishedAt: &publishedTime,
			AuthorID:    "user_456",
			AuthorName:  "Jane Doe",
			AuthorEmail: "jane@example.com",
			Tags:        []string{"security", "api", "authentication"},
			WordCount:   1800,
			Language:    "en",
			Category:    "documentation",
		},
		{
			ID:          "doc_006",
			Title:       "Mobile App Integration Notes",
			Content:     "Draft notes for iOS and Android API integration.",
			Status:      "draft",
			CreatedAt:   now.Add(-3 * 24 * time.Hour),
			UpdatedAt:   now.Add(-6 * time.Hour),
			PublishedAt: nil,
			AuthorID:    "user_123",
			AuthorName:  "John Smith",
			AuthorEmail: "john@example.com",
			Tags:        []string{"mobile", "api", "ios", "android"},
			WordCount:   450,
			Language:    "en",
			Category:    "notes",
		},
	}
}

func (t *DocumentSearchTool) Execute(params map[string]interface{}) (interface{}, error) {
	// Extract parameters
	query, _ := params["query"].(string)
	filters, _ := params["filters"].(map[string]interface{})
	sortParams, _ := params["sort"].([]interface{})
	paginationParams, _ := params["pagination"].(map[string]interface{})
	includeParams, _ := params["include"].([]interface{})

	// Log received parameters for debugging
	log.Printf("📄 Document Search Tool - Received params:")
	log.Printf("  Query: %s", query)
	log.Printf("  Filters: %+v", filters)
	log.Printf("  Sort: %+v", sortParams)
	log.Printf("  Pagination: %+v", paginationParams)
	log.Printf("  Include: %+v", includeParams)

	// Extract filter values
	var statusFilter []string
	var dateRange map[string]interface{}
	var metadataFilters []interface{}
	var tagsFilter map[string]interface{}
	var authorFilter map[string]interface{}

	if filters != nil {
		if status, ok := filters["status"].([]interface{}); ok {
			for _, s := range status {
				if str, ok := s.(string); ok {
					statusFilter = append(statusFilter, str)
				}
			}
		}
		if dr, ok := filters["date_range"].(map[string]interface{}); ok {
			dateRange = dr
		}
		if meta, ok := filters["metadata"].([]interface{}); ok {
			metadataFilters = meta
		}
		if tags, ok := filters["tags"].(map[string]interface{}); ok {
			tagsFilter = tags
		}
		if author, ok := filters["author"].(map[string]interface{}); ok {
			authorFilter = author
		}
	}

	// Get mock documents and apply filters
	docs := getMockDocuments()
	var filteredDocs []mockDocument

	for _, doc := range docs {
		// Apply query filter (search in title and content)
		if query != "" {
			queryLower := strings.ToLower(query)
			if !strings.Contains(strings.ToLower(doc.Title), queryLower) &&
				!strings.Contains(strings.ToLower(doc.Content), queryLower) {
				continue
			}
		}

		// Apply status filter
		if len(statusFilter) > 0 {
			matched := false
			for _, s := range statusFilter {
				if doc.Status == s {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		// Apply date range filter
		if dateRange != nil {
			field, _ := dateRange["field"].(string)
			startStr, _ := dateRange["start"].(string)
			endStr, _ := dateRange["end"].(string)

			var docTime time.Time
			switch field {
			case "created_at":
				docTime = doc.CreatedAt
			case "updated_at":
				docTime = doc.UpdatedAt
			case "published_at":
				if doc.PublishedAt != nil {
					docTime = *doc.PublishedAt
				} else {
					continue // No published date, skip
				}
			}

			if startStr != "" {
				if start, err := time.Parse(time.RFC3339, startStr); err == nil {
					if docTime.Before(start) {
						continue
					}
				}
			}
			if endStr != "" {
				if end, err := time.Parse(time.RFC3339, endStr); err == nil {
					if docTime.After(end) {
						continue
					}
				}
			}
		}

		// Apply tags filter
		if tagsFilter != nil {
			matchType, _ := tagsFilter["match"].(string)
			var tagValues []string
			if vals, ok := tagsFilter["values"].([]interface{}); ok {
				for _, v := range vals {
					if str, ok := v.(string); ok {
						tagValues = append(tagValues, str)
					}
				}
			}

			if len(tagValues) > 0 {
				switch matchType {
				case "all":
					// Must have all tags
					hasAll := true
					for _, tag := range tagValues {
						found := false
						for _, docTag := range doc.Tags {
							if docTag == tag {
								found = true
								break
							}
						}
						if !found {
							hasAll = false
							break
						}
					}
					if !hasAll {
						continue
					}
				case "any":
					// Must have at least one tag
					hasAny := false
					for _, tag := range tagValues {
						for _, docTag := range doc.Tags {
							if docTag == tag {
								hasAny = true
								break
							}
						}
						if hasAny {
							break
						}
					}
					if !hasAny {
						continue
					}
				case "none":
					// Must have none of the tags
					hasNone := true
					for _, tag := range tagValues {
						for _, docTag := range doc.Tags {
							if docTag == tag {
								hasNone = false
								break
							}
						}
						if !hasNone {
							break
						}
					}
					if !hasNone {
						continue
					}
				}
			}
		}

		// Apply author filter
		if authorFilter != nil {
			if id, ok := authorFilter["id"].(string); ok && id != "" {
				if doc.AuthorID != id {
					continue
				}
			}
			if name, ok := authorFilter["name"].(string); ok && name != "" {
				if !strings.Contains(strings.ToLower(doc.AuthorName), strings.ToLower(name)) {
					continue
				}
			}
			if email, ok := authorFilter["email"].(string); ok && email != "" {
				if doc.AuthorEmail != email {
					continue
				}
			}
		}

		// Apply metadata filters
		if len(metadataFilters) > 0 {
			passedAll := true
			for _, mf := range metadataFilters {
				if m, ok := mf.(map[string]interface{}); ok {
					key, _ := m["key"].(string)
					operator, _ := m["operator"].(string)
					value := m["value"]

					var docValue interface{}
					switch key {
					case "word_count":
						docValue = doc.WordCount
					case "language":
						docValue = doc.Language
					case "category":
						docValue = doc.Category
					default:
						continue
					}

					if !applyMetadataFilter(docValue, operator, value) {
						passedAll = false
						break
					}
				}
			}
			if !passedAll {
				continue
			}
		}

		filteredDocs = append(filteredDocs, doc)
	}

	// Apply sorting
	if len(sortParams) > 0 {
		for i := len(sortParams) - 1; i >= 0; i-- {
			if sortObj, ok := sortParams[i].(map[string]interface{}); ok {
				field, _ := sortObj["field"].(string)
				direction, _ := sortObj["direction"].(string)
				sortDocuments(filteredDocs, field, direction == "desc")
			}
		}
	}

	// Apply pagination
	page := 1
	perPage := 10
	if paginationParams != nil {
		if p, ok := paginationParams["page"].(float64); ok {
			page = int(p)
		}
		if pp, ok := paginationParams["per_page"].(float64); ok {
			perPage = int(pp)
		}
	}

	totalDocs := len(filteredDocs)
	totalPages := (totalDocs + perPage - 1) / perPage
	if totalPages == 0 {
		totalPages = 1
	}

	startIdx := (page - 1) * perPage
	endIdx := startIdx + perPage
	if startIdx > len(filteredDocs) {
		startIdx = len(filteredDocs)
	}
	if endIdx > len(filteredDocs) {
		endIdx = len(filteredDocs)
	}
	paginatedDocs := filteredDocs[startIdx:endIdx]

	// Build include set
	includeSet := make(map[string]bool)
	for _, inc := range includeParams {
		if str, ok := inc.(string); ok {
			includeSet[str] = true
		}
	}

	// Convert to response format
	var results []map[string]interface{}
	for _, doc := range paginatedDocs {
		result := map[string]interface{}{
			"id":         doc.ID,
			"title":      doc.Title,
			"status":     doc.Status,
			"created_at": doc.CreatedAt.Format(time.RFC3339),
			"updated_at": doc.UpdatedAt.Format(time.RFC3339),
		}

		// Include author if requested or by default
		if len(includeSet) == 0 || includeSet["author"] {
			result["author"] = map[string]interface{}{
				"id":    doc.AuthorID,
				"name":  doc.AuthorName,
				"email": doc.AuthorEmail,
			}
		}

		// Include metadata if requested or by default
		if len(includeSet) == 0 || includeSet["metadata"] {
			result["metadata"] = map[string]interface{}{
				"word_count": doc.WordCount,
				"language":   doc.Language,
				"category":   doc.Category,
			}
			result["tags"] = doc.Tags
		}

		results = append(results, result)
	}

	response := map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Found %d documents matching your search", totalDocs),
		"filters_applied": map[string]interface{}{
			"query":            query,
			"status_filter":    statusFilter,
			"date_range":       dateRange,
			"metadata_filters": metadataFilters,
			"tags_filter":      tagsFilter,
			"author_filter":    authorFilter,
		},
		"results": results,
		"pagination": map[string]interface{}{
			"page":        page,
			"per_page":    perPage,
			"total":       totalDocs,
			"total_pages": totalPages,
		},
	}

	return response, nil
}

// applyMetadataFilter applies a metadata filter condition
func applyMetadataFilter(docValue interface{}, operator string, filterValue interface{}) bool {
	switch operator {
	case "equals":
		return fmt.Sprintf("%v", docValue) == fmt.Sprintf("%v", filterValue)
	case "not_equals":
		return fmt.Sprintf("%v", docValue) != fmt.Sprintf("%v", filterValue)
	case "contains":
		return strings.Contains(fmt.Sprintf("%v", docValue), fmt.Sprintf("%v", filterValue))
	case "starts_with":
		return strings.HasPrefix(fmt.Sprintf("%v", docValue), fmt.Sprintf("%v", filterValue))
	case "ends_with":
		return strings.HasSuffix(fmt.Sprintf("%v", docValue), fmt.Sprintf("%v", filterValue))
	case "gt", "gte", "lt", "lte":
		docNum, ok1 := toFloat64(docValue)
		filterNum, ok2 := toFloat64(filterValue)
		if !ok1 || !ok2 {
			return false
		}
		switch operator {
		case "gt":
			return docNum > filterNum
		case "gte":
			return docNum >= filterNum
		case "lt":
			return docNum < filterNum
		case "lte":
			return docNum <= filterNum
		}
	}
	return true
}

// toFloat64 converts a value to float64
func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case string:
		if f, err := fmt.Sscanf(val, "%f"); err == nil {
			return float64(f), true
		}
	}
	return 0, false
}

// sortDocuments sorts documents by a field
func sortDocuments(docs []mockDocument, field string, descending bool) {
	for i := 0; i < len(docs)-1; i++ {
		for j := i + 1; j < len(docs); j++ {
			shouldSwap := false
			switch field {
			case "created_at":
				if descending {
					shouldSwap = docs[i].CreatedAt.Before(docs[j].CreatedAt)
				} else {
					shouldSwap = docs[i].CreatedAt.After(docs[j].CreatedAt)
				}
			case "updated_at":
				if descending {
					shouldSwap = docs[i].UpdatedAt.Before(docs[j].UpdatedAt)
				} else {
					shouldSwap = docs[i].UpdatedAt.After(docs[j].UpdatedAt)
				}
			case "title":
				if descending {
					shouldSwap = docs[i].Title < docs[j].Title
				} else {
					shouldSwap = docs[i].Title > docs[j].Title
				}
			case "word_count":
				if descending {
					shouldSwap = docs[i].WordCount < docs[j].WordCount
				} else {
					shouldSwap = docs[i].WordCount > docs[j].WordCount
				}
			}
			if shouldSwap {
				docs[i], docs[j] = docs[j], docs[i]
			}
		}
	}
}
