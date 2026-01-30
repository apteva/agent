package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apteva/agent/config"
	"github.com/apteva/agent/handlers/threads"
)

func TestHealthEndpoint(t *testing.T) {
	testServer := NewTestServer(t)
	defer testServer.Close()

	t.Run("GET /health", func(t *testing.T) {
		resp, err := testServer.Get("/health")
		AssertNil(t, err)
		defer resp.Body.Close()

		AssertEqual(t, http.StatusOK, resp.StatusCode)

		var healthResp HealthResponse
		err = json.NewDecoder(resp.Body).Decode(&healthResp)
		AssertNil(t, err)

		AssertEqual(t, "healthy", healthResp.Status)
		AssertEqual(t, "1.4.0", healthResp.Version)
		AssertNotNil(t, healthResp.Uptime)
	})

	t.Run("Non-GET /health", func(t *testing.T) {
		// Create custom request with POST method
		req, err := http.NewRequest("POST", testServer.Server.URL+"/health", nil)
		AssertNil(t, err)

		client := &http.Client{}
		resp, err := client.Do(req)
		AssertNil(t, err)
		defer resp.Body.Close()

		AssertEqual(t, http.StatusMethodNotAllowed, resp.StatusCode)
	})
}

func TestConfigEndpoint(t *testing.T) {
	testServer := NewTestServer(t)
	defer testServer.Close()

	t.Run("GET /config", func(t *testing.T) {
		resp, err := testServer.Get("/config")
		AssertNil(t, err)
		defer resp.Body.Close()

		AssertEqual(t, http.StatusOK, resp.StatusCode)

		var agentConfig config.AgentConfig
		err = json.NewDecoder(resp.Body).Decode(&agentConfig)
		AssertNil(t, err)

		AssertNotNil(t, agentConfig.Name)
		AssertNotNil(t, agentConfig.LLM.Provider)
		AssertNotNil(t, agentConfig.LLM.Model)
	})

	t.Run("POST /config - Update Provider", func(t *testing.T) {
		updateData := map[string]interface{}{
			"llm": map[string]interface{}{
				"provider": "openai",
				"model":    "gpt-4",
			},
		}

		resp, err := testServer.PostJSON("/config", updateData)
		AssertNil(t, err)
		defer resp.Body.Close()

		AssertEqual(t, http.StatusOK, resp.StatusCode)

		// Verify the update
		getResp, err := testServer.Get("/config")
		AssertNil(t, err)
		defer getResp.Body.Close()

		var updatedConfig config.AgentConfig
		err = json.NewDecoder(getResp.Body).Decode(&updatedConfig)
		AssertNil(t, err)

		AssertEqual(t, "openai", updatedConfig.LLM.Provider)
		AssertEqual(t, "gpt-4", updatedConfig.LLM.Model)
	})

	t.Run("POST /config - Update Tools", func(t *testing.T) {
		updateData := map[string]interface{}{
			"llm": map[string]interface{}{
				"tools": []string{"get_time", "send_notification"},
				"builtin_tools": []interface{}{
					map[string]interface{}{
						"type": "web_fetch_20250910",
						"name": "web_fetch",
					},
				},
			},
		}

		resp, err := testServer.PostJSON("/config", updateData)
		AssertNil(t, err)
		defer resp.Body.Close()

		AssertEqual(t, http.StatusOK, resp.StatusCode)

		// Verify the update
		getResp, err := testServer.Get("/config")
		AssertNil(t, err)
		defer getResp.Body.Close()

		var updatedConfig config.AgentConfig
		err = json.NewDecoder(getResp.Body).Decode(&updatedConfig)
		AssertNil(t, err)

		// Tools may include auto-added operator tools if operator mode is enabled
		AssertEqual(t, true, len(updatedConfig.LLM.Tools) >= 2)
		// Check that our tools are present
		hasGetTime := false
		hasSendNotification := false
		for _, tool := range updatedConfig.LLM.Tools {
			if tool == "get_time" {
				hasGetTime = true
			}
			if tool == "send_notification" {
				hasSendNotification = true
			}
		}
		AssertEqual(t, true, hasGetTime)
		AssertEqual(t, true, hasSendNotification)
		// Builtin tools may include auto-added computer tool if operator mode is enabled
		AssertEqual(t, true, len(updatedConfig.LLM.BuiltinTools) >= 1)
	})

	t.Run("POST /config - Invalid JSON", func(t *testing.T) {
		invalidJSON := `{"invalid": json}`
		resp, err := http.Post(
			testServer.Server.URL+"/config",
			"application/json",
			strings.NewReader(invalidJSON),
		)
		AssertNil(t, err)
		defer resp.Body.Close()

		AssertEqual(t, http.StatusBadRequest, resp.StatusCode)
	})
}

func TestChatEndpoint(t *testing.T) {
	testServer := NewTestServer(t)
	defer testServer.Close()

	t.Run("POST /chat - Simple Message", func(t *testing.T) {
		chatReq := ChatRequest{
			Message: "Hello, world!",
		}

		resp, err := testServer.PostJSON("/chat", chatReq)
		AssertNil(t, err)
		defer resp.Body.Close()

		AssertEqual(t, http.StatusOK, resp.StatusCode)
		AssertEqual(t, "text/event-stream", resp.Header.Get("Content-Type"))

		// Check for thread ID header
		threadID := resp.Header.Get("X-Thread-ID")
		AssertNotNil(t, threadID)
		AssertEqual(t, true, len(threadID) > 0)

		// Read streaming response
		bodyBytes, err := io.ReadAll(resp.Body)
		AssertNil(t, err)
		body := string(bodyBytes)
		AssertContains(t, body, "data:")
		AssertContains(t, body, "Test response")
	})

	t.Run("POST /chat - With Thread ID", func(t *testing.T) {
		// First, create a thread by making an initial request
		chatReq1 := ChatRequest{
			Message: "First message",
		}

		resp1, err := testServer.PostJSON("/chat", chatReq1)
		AssertNil(t, err)
		defer resp1.Body.Close()

		threadID := resp1.Header.Get("X-Thread-ID")
		AssertNotNil(t, threadID)

		// Make second request with same thread ID
		chatReq2 := ChatRequest{
			Message:  "Second message",
			ThreadID: &threadID,
		}

		resp2, err := testServer.PostJSON("/chat", chatReq2)
		AssertNil(t, err)
		defer resp2.Body.Close()

		AssertEqual(t, http.StatusOK, resp2.StatusCode)
		AssertEqual(t, threadID, resp2.Header.Get("X-Thread-ID"))
	})

	t.Run("POST /chat - Content Blocks", func(t *testing.T) {
		contentBlocks := []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": "Hello from content blocks",
			},
		}

		chatReq := ChatRequest{
			Message: contentBlocks,
		}

		resp, err := testServer.PostJSON("/chat", chatReq)
		AssertNil(t, err)
		defer resp.Body.Close()

		AssertEqual(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("POST /chat - Raw Mode", func(t *testing.T) {
		rawMode := true
		chatReq := ChatRequest{
			Message: "Raw mode test",
			RawMode: &rawMode,
		}

		resp, err := testServer.PostJSON("/chat", chatReq)
		AssertNil(t, err)
		defer resp.Body.Close()

		AssertEqual(t, http.StatusOK, resp.StatusCode)
		// In raw mode, content type should be text/plain (set by mock handler)
	})

	t.Run("POST /chat - Empty Message", func(t *testing.T) {
		chatReq := ChatRequest{
			Message: "",
		}

		resp, err := testServer.PostJSON("/chat", chatReq)
		AssertNil(t, err)
		defer resp.Body.Close()

		AssertEqual(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("POST /chat - Invalid Content Block", func(t *testing.T) {
		invalidBlocks := []interface{}{
			"not a content block object",
		}

		chatReq := ChatRequest{
			Message: invalidBlocks,
		}

		resp, err := testServer.PostJSON("/chat", chatReq)
		AssertNil(t, err)
		defer resp.Body.Close()

		AssertEqual(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("GET /chat - Method Not Allowed", func(t *testing.T) {
		resp, err := testServer.Get("/chat")
		AssertNil(t, err)
		defer resp.Body.Close()

		AssertEqual(t, http.StatusMethodNotAllowed, resp.StatusCode)
	})
}

func TestThreadsEndpoint(t *testing.T) {
	testServer := NewTestServer(t)
	defer testServer.Close()

	// Create a custom handler that includes thread endpoints
	mux := http.NewServeMux()
	mux.Handle("/threads", threads.HandleThreads(testServer.TestDB.DB))
	mux.Handle("/threads/", threads.HandleThreadMessages(testServer.TestDB.DB))

	server := httptest.NewServer(mux)
	defer server.Close()

	// We need to set up the database connection for the handlers
	db = testServer.TestDB.DB

	t.Run("GET /threads - Empty", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/threads")
		AssertNil(t, err)
		defer resp.Body.Close()

		AssertEqual(t, http.StatusOK, resp.StatusCode)

		var threads []Thread
		err = json.NewDecoder(resp.Body).Decode(&threads)
		AssertNil(t, err)
		AssertEqual(t, 0, len(threads))
	})

	t.Run("GET /threads - With Data", func(t *testing.T) {
		// Create test threads
		saver := testServer.TestDB.GetMessageSaver()
		threadID1 := CreateTestThread(t, saver)
		threadID2 := CreateTestThread(t, saver)

		CreateTestMessage(t, saver, threadID1, "user", "Hello")
		CreateTestMessage(t, saver, threadID2, "user", "Hi there")

		resp, err := http.Get(server.URL + "/threads")
		AssertNil(t, err)
		defer resp.Body.Close()

		AssertEqual(t, http.StatusOK, resp.StatusCode)

		var threads []Thread
		err = json.NewDecoder(resp.Body).Decode(&threads)
		AssertNil(t, err)
		AssertEqual(t, 2, len(threads))

		// Threads should be ordered by updated_at DESC
		AssertNotNil(t, threads[0].ID)
		AssertNotNil(t, threads[1].ID)
	})

	t.Run("GET /threads/{id}/messages", func(t *testing.T) {
		// Create test thread with messages
		saver := testServer.TestDB.GetMessageSaver()
		threadID := CreateTestThread(t, saver)

		CreateTestMessage(t, saver, threadID, "user", "Hello")
		CreateTestMessage(t, saver, threadID, "assistant", "Hi there!")

		resp, err := http.Get(server.URL + "/threads/" + threadID + "/messages")
		AssertNil(t, err)
		defer resp.Body.Close()

		AssertEqual(t, http.StatusOK, resp.StatusCode)

		var messages []Message
		err = json.NewDecoder(resp.Body).Decode(&messages)
		AssertNil(t, err)
		AssertEqual(t, 2, len(messages))

		// Messages should be in chronological order
		AssertEqual(t, "user", messages[0].Role)
		AssertEqual(t, "Hello", messages[0].Content)
		AssertEqual(t, "assistant", messages[1].Role)
		AssertEqual(t, "Hi there!", messages[1].Content)
	})

	t.Run("GET /threads/{id}/messages - Invalid Path", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/threads/")
		AssertNil(t, err)
		defer resp.Body.Close()

		AssertEqual(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("POST /threads - Method Not Allowed", func(t *testing.T) {
		resp, err := http.Post(server.URL+"/threads", "application/json", nil)
		AssertNil(t, err)
		defer resp.Body.Close()

		AssertEqual(t, http.StatusMethodNotAllowed, resp.StatusCode)
	})
}

func TestResetEndpoint(t *testing.T) {
	testServer := NewTestServer(t)
	defer testServer.Close()

	// Create a custom handler for reset
	mux := http.NewServeMux()
	mux.HandleFunc("/reset", func(w http.ResponseWriter, r *http.Request) {
		handleReset(w, r)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// Set up database connection
	db = testServer.TestDB.DB

	t.Run("POST /reset", func(t *testing.T) {
		// First, add some data
		saver := testServer.TestDB.GetMessageSaver()
		threadID := CreateTestThread(t, saver)
		CreateTestMessage(t, saver, threadID, "user", "Test message")

		// Verify data exists
		messages, err := saver.GetThreadMessages(threadID)
		AssertNil(t, err)
		AssertEqual(t, 1, len(messages))

		// Reset database
		resp, err := http.Post(server.URL+"/reset", "application/json", nil)
		AssertNil(t, err)
		defer resp.Body.Close()

		AssertEqual(t, http.StatusOK, resp.StatusCode)

		var result map[string]string
		err = json.NewDecoder(resp.Body).Decode(&result)
		AssertNil(t, err)
		AssertEqual(t, "Database cleared successfully", result["message"])
		AssertEqual(t, "completed", result["status"])

		// Verify data is gone
		messages, err = saver.GetThreadMessages(threadID)
		AssertNil(t, err)
		AssertEqual(t, 0, len(messages))
	})

	t.Run("GET /reset - Method Not Allowed", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/reset")
		AssertNil(t, err)
		defer resp.Body.Close()

		AssertEqual(t, http.StatusMethodNotAllowed, resp.StatusCode)
	})
}

func TestWebInterfaceEndpoint(t *testing.T) {
	testServer := NewTestServer(t)
	defer testServer.Close()

	// Create a custom handler for web interface
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handleWebInterface(w, r)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	t.Run("GET / - No Web File", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/")
		AssertNil(t, err)
		defer resp.Body.Close()

		AssertEqual(t, http.StatusOK, resp.StatusCode)
		AssertEqual(t, "application/json", resp.Header.Get("Content-Type"))

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		AssertNil(t, err)
		AssertEqual(t, "Agent Go API Server", result["message"])
		AssertEqual(t, "1.0.0", result["version"])
		AssertNotNil(t, result["endpoints"])
	})

	t.Run("GET /nonexistent - 404", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/nonexistent")
		AssertNil(t, err)
		defer resp.Body.Close()

		AssertEqual(t, http.StatusNotFound, resp.StatusCode)
	})
}

func TestAPIErrorHandling(t *testing.T) {
	testServer := NewTestServer(t)
	defer testServer.Close()

	t.Run("Invalid JSON Body", func(t *testing.T) {
		invalidJSON := `{"invalid": json`
		resp, err := http.Post(
			testServer.Server.URL+"/chat",
			"application/json",
			strings.NewReader(invalidJSON),
		)
		AssertNil(t, err)
		defer resp.Body.Close()

		AssertEqual(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("Missing Content-Type", func(t *testing.T) {
		chatReq := ChatRequest{Message: "test"}
		jsonData, _ := json.Marshal(chatReq)

		resp, err := http.Post(
			testServer.Server.URL+"/chat",
			"text/plain", // Wrong content type
			bytes.NewBuffer(jsonData),
		)
		AssertNil(t, err)
		defer resp.Body.Close()

		// Should still work as Go's JSON decoder is flexible
		AssertEqual(t, http.StatusOK, resp.StatusCode)
	})
}

func TestConcurrentAPIRequests(t *testing.T) {
	testServer := NewTestServer(t)
	defer testServer.Close()

	t.Run("ConcurrentHealthChecks", func(t *testing.T) {
		numRequests := 10
		done := make(chan bool, numRequests)

		for i := 0; i < numRequests; i++ {
			go func(index int) {
				defer func() { done <- true }()

				resp, err := testServer.Get("/health")
				if err != nil {
					t.Errorf("Request %d failed: %v", index, err)
					return
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					t.Errorf("Request %d got status %d", index, resp.StatusCode)
				}
			}(i)
		}

		for i := 0; i < numRequests; i++ {
			<-done
		}
	})

	t.Run("ConcurrentChatRequests", func(t *testing.T) {
		numRequests := 5
		done := make(chan string, numRequests)

		for i := 0; i < numRequests; i++ {
			go func(index int) {
				defer func() {
					done <- fmt.Sprintf("request_%d_done", index)
				}()

				chatReq := ChatRequest{
					Message: fmt.Sprintf("Concurrent message %d", index),
				}

				resp, err := testServer.PostJSON("/chat", chatReq)
				if err != nil {
					t.Errorf("Chat request %d failed: %v", index, err)
					return
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					t.Errorf("Chat request %d got status %d", index, resp.StatusCode)
				}

				// Verify thread ID is returned
				threadID := resp.Header.Get("X-Thread-ID")
				if threadID == "" {
					t.Errorf("Chat request %d missing thread ID", index)
				}
			}(i)
		}

		// Collect all responses
		responses := make([]string, 0, numRequests)
		for i := 0; i < numRequests; i++ {
			responses = append(responses, <-done)
		}

		AssertEqual(t, numRequests, len(responses))
	})
}

func TestAPIMiddleware(t *testing.T) {
	t.Run("LoggingMiddleware", func(t *testing.T) {
		// Create a test handler
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		})

		// Wrap with logging middleware
		wrappedHandler := loggingMiddleware(handler)

		// Create test request
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()

		// Execute request
		wrappedHandler.ServeHTTP(recorder, req)

		// Verify response
		AssertEqual(t, http.StatusOK, recorder.Code)
		AssertEqual(t, "OK", recorder.Body.String())
	})
}

func TestAPIHeaders(t *testing.T) {
	testServer := NewTestServer(t)
	defer testServer.Close()

	t.Run("CORSHeaders", func(t *testing.T) {
		chatReq := ChatRequest{Message: "test"}
		resp, err := testServer.PostJSON("/chat", chatReq)
		AssertNil(t, err)
		defer resp.Body.Close()

		// Check CORS headers in streaming response
		AssertEqual(t, "*", resp.Header.Get("Access-Control-Allow-Origin"))
	})

	t.Run("ContentTypeHeaders", func(t *testing.T) {
		// Test JSON response
		resp, err := testServer.Get("/health")
		AssertNil(t, err)
		defer resp.Body.Close()

		AssertEqual(t, "application/json", resp.Header.Get("Content-Type"))

		// Test streaming response
		chatReq := ChatRequest{Message: "test"}
		streamResp, err := testServer.PostJSON("/chat", chatReq)
		AssertNil(t, err)
		defer streamResp.Body.Close()

		AssertEqual(t, "text/event-stream", streamResp.Header.Get("Content-Type"))
	})
}

func BenchmarkHealthEndpoint(b *testing.B) {
	testServer := NewTestServer(&testing.T{})
	defer testServer.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := testServer.Get("/health")
		if err != nil {
			b.Fatalf("Request failed: %v", err)
		}
		resp.Body.Close()
	}
}

func BenchmarkConfigEndpoint(b *testing.B) {
	testServer := NewTestServer(&testing.T{})
	defer testServer.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := testServer.Get("/config")
		if err != nil {
			b.Fatalf("Request failed: %v", err)
		}
		resp.Body.Close()
	}
}