package tools

import (
	"context"
	"fmt"
	"log"

	"github.com/apteva/agent/config"
	"github.com/apteva/agent/filesystem"
)

// Global file manager reference (set by main.go)
var globalFileManager *filesystem.FileManager

// SetFileManager sets the global file manager for filesystem tools
func SetFileManager(fm *filesystem.FileManager) {
	globalFileManager = fm
}

// ListFiles lists files in storage with optional filters
func ListFiles(threadID, fileType string, limit int) map[string]interface{} {
	if globalFileManager == nil || !globalFileManager.IsEnabled() {
		return map[string]interface{}{
			"success": false,
			"error":   "Filesystem is not enabled",
		}
	}

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	files, err := globalFileManager.ListFilesFiltered(threadID, fileType, limit)
	if err != nil {
		log.Printf("Error listing files: %v", err)
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("Failed to list files: %v", err),
		}
	}

	// Convert to response format with public URLs
	fileList := make([]map[string]interface{}, 0, len(files))
	for _, f := range files {
		fileInfo := map[string]interface{}{
			"file_id":    f.ID,
			"filename":   f.Filename,
			"mime_type":  f.MimeType,
			"file_type":  f.FileType,
			"size_bytes": f.SizeBytes,
			"thread_id":  f.ThreadID,
			"created_at": f.CreatedAt,
		}

		// Add public URL if available
		if url, err := globalFileManager.GetPublicURL(f.ID); err == nil && url != "" {
			fileInfo["url"] = url
		}

		fileList = append(fileList, fileInfo)
	}

	return map[string]interface{}{
		"success": true,
		"files":   fileList,
		"count":   len(fileList),
	}
}

// GetFile gets file metadata and public URL by ID
func GetFile(fileID string) map[string]interface{} {
	if globalFileManager == nil || !globalFileManager.IsEnabled() {
		return map[string]interface{}{
			"success": false,
			"error":   "Filesystem is not enabled",
		}
	}

	if fileID == "" {
		return map[string]interface{}{
			"success": false,
			"error":   "File ID is required",
		}
	}

	file, err := globalFileManager.GetMetadata(fileID)
	if err != nil {
		log.Printf("Error getting file %s: %v", fileID, err)
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("File not found: %v", err),
		}
	}

	result := map[string]interface{}{
		"success":    true,
		"file_id":    file.ID,
		"filename":   file.Filename,
		"mime_type":  file.MimeType,
		"file_type":  file.FileType,
		"size_bytes": file.SizeBytes,
		"thread_id":  file.ThreadID,
		"message_id": file.MessageID,
		"checksum":   file.Checksum,
		"created_at": file.CreatedAt,
		"metadata":   file.Metadata,
	}

	// Add public URL if available
	if url, err := globalFileManager.GetPublicURL(fileID); err == nil && url != "" {
		result["url"] = url
	}

	return result
}

// DeleteFile deletes a specific file
func DeleteFile(fileID string) map[string]interface{} {
	if globalFileManager == nil || !globalFileManager.IsEnabled() {
		return map[string]interface{}{
			"success": false,
			"error":   "Filesystem is not enabled",
		}
	}

	if fileID == "" {
		return map[string]interface{}{
			"success": false,
			"error":   "File ID is required",
		}
	}

	err := globalFileManager.Delete(context.Background(), fileID)
	if err != nil {
		log.Printf("Error deleting file %s: %v", fileID, err)
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("Failed to delete file: %v", err),
		}
	}

	log.Printf("✅ Deleted file: %s", fileID)

	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("File %s deleted successfully", fileID),
	}
}

// SearchFiles searches files by query
func SearchFiles(query, fileType string, limit int) map[string]interface{} {
	if globalFileManager == nil || !globalFileManager.IsEnabled() {
		return map[string]interface{}{
			"success": false,
			"error":   "Filesystem is not enabled",
		}
	}

	if query == "" {
		return map[string]interface{}{
			"success": false,
			"error":   "Search query is required",
		}
	}

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	files, err := globalFileManager.SearchFiles(query, fileType, limit)
	if err != nil {
		log.Printf("Error searching files: %v", err)
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("Failed to search files: %v", err),
		}
	}

	// Convert to response format with public URLs
	fileList := make([]map[string]interface{}, 0, len(files))
	for _, f := range files {
		fileInfo := map[string]interface{}{
			"file_id":    f.ID,
			"filename":   f.Filename,
			"mime_type":  f.MimeType,
			"file_type":  f.FileType,
			"size_bytes": f.SizeBytes,
			"thread_id":  f.ThreadID,
			"created_at": f.CreatedAt,
		}

		// Add public URL if available
		if url, err := globalFileManager.GetPublicURL(f.ID); err == nil && url != "" {
			fileInfo["url"] = url
		}

		fileList = append(fileList, fileInfo)
	}

	return map[string]interface{}{
		"success": true,
		"files":   fileList,
		"count":   len(fileList),
		"query":   query,
	}
}

// Tool wrappers for registration

// ListFilesToolWrapper wraps the list files function
type ListFilesToolWrapper struct{}

func (t *ListFilesToolWrapper) Name() string {
	return "list_files"
}

func (t *ListFilesToolWrapper) DisplayName() string {
	return "List Files"
}

func (t *ListFilesToolWrapper) Description() string {
	return "List files stored in the agent's filesystem. Can filter by thread ID and file type. Returns file metadata and public URLs."
}

func (t *ListFilesToolWrapper) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"thread_id": map[string]interface{}{
				"type":        "string",
				"description": "Filter files by thread ID (optional)",
			},
			"file_type": map[string]interface{}{
				"type":        "string",
				"description": "Filter by file type: 'image', 'audio', 'document', 'video'",
				"enum":        []string{"image", "audio", "document", "video"},
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of files to return (default: 20, max: 100)",
				"default":     20,
			},
		},
	}
}

func (t *ListFilesToolWrapper) Execute(params map[string]interface{}) (interface{}, error) {
	threadID, _ := params["thread_id"].(string)
	fileType, _ := params["file_type"].(string)
	limit := 20
	if l, ok := params["limit"].(float64); ok {
		limit = int(l)
	}
	return ListFiles(threadID, fileType, limit), nil
}

// GetFileToolWrapper wraps the get file function
type GetFileToolWrapper struct{}

func (t *GetFileToolWrapper) Name() string {
	return "get_file"
}

func (t *GetFileToolWrapper) DisplayName() string {
	return "Get File"
}

func (t *GetFileToolWrapper) Description() string {
	return "Get detailed information about a specific file including its public URL for sharing."
}

func (t *GetFileToolWrapper) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"file_id": map[string]interface{}{
				"type":        "string",
				"description": "The ID of the file to retrieve",
			},
		},
		"required": []string{"file_id"},
	}
}

func (t *GetFileToolWrapper) Execute(params map[string]interface{}) (interface{}, error) {
	fileID, _ := params["file_id"].(string)
	return GetFile(fileID), nil
}

// DeleteFileToolWrapper wraps the delete file function
type DeleteFileToolWrapper struct{}

func (t *DeleteFileToolWrapper) Name() string {
	return "delete_file"
}

func (t *DeleteFileToolWrapper) DisplayName() string {
	return "Delete File"
}

func (t *DeleteFileToolWrapper) Description() string {
	return "Delete a specific file from the agent's filesystem."
}

func (t *DeleteFileToolWrapper) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"file_id": map[string]interface{}{
				"type":        "string",
				"description": "The ID of the file to delete",
			},
		},
		"required": []string{"file_id"},
	}
}

func (t *DeleteFileToolWrapper) Execute(params map[string]interface{}) (interface{}, error) {
	fileID, _ := params["file_id"].(string)
	return DeleteFile(fileID), nil
}

// SearchFilesToolWrapper wraps the search files function
type SearchFilesToolWrapper struct{}

func (t *SearchFilesToolWrapper) Name() string {
	return "search_files"
}

func (t *SearchFilesToolWrapper) DisplayName() string {
	return "Search Files"
}

func (t *SearchFilesToolWrapper) Description() string {
	return "Search for files by filename or metadata. Returns matching files with their public URLs."
}

func (t *SearchFilesToolWrapper) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search query to match against filenames",
			},
			"file_type": map[string]interface{}{
				"type":        "string",
				"description": "Filter by file type: 'image', 'audio', 'document', 'video'",
				"enum":        []string{"image", "audio", "document", "video"},
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of results (default: 20, max: 100)",
				"default":     20,
			},
		},
		"required": []string{"query"},
	}
}

func (t *SearchFilesToolWrapper) Execute(params map[string]interface{}) (interface{}, error) {
	query, _ := params["query"].(string)
	fileType, _ := params["file_type"].(string)
	limit := 20
	if l, ok := params["limit"].(float64); ok {
		limit = int(l)
	}
	return SearchFiles(query, fileType, limit), nil
}

// RegisterFilesystemTools registers all filesystem tools
func RegisterFilesystemTools() {
	cfg := config.GetConfig()
	fsConfig := cfg.Get().FileSystem

	if fsConfig == nil || !fsConfig.Enabled {
		return
	}

	registry := GetGlobalRegistry()
	registry.RegisterTool(&ListFilesToolWrapper{})
	registry.RegisterTool(&GetFileToolWrapper{})
	registry.RegisterTool(&DeleteFileToolWrapper{})
	registry.RegisterTool(&SearchFilesToolWrapper{})

	log.Printf("📁 Registered filesystem tools: list_files, get_file, delete_file, search_files")
}
