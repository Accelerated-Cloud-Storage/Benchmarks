package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"time"
)

// Represents a single request within the batch file (JSONL format).
type BatchRequestItem struct {
	CustomID string `json:"custom_id"`
	Method   string `json:"method"`
	URL      string `json:"url"`
	Body     any    `json:"body"`
}

// Represents the body for a chat completion request.
type ChatCompletionBody struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

// Represents the body for a /v1/responses request.
type ResponseBody struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

// Represents a message in the chat completion request.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Represents the response from the file upload endpoint.
type FileUploadResponse struct {
	ID        string `json:"id"`
	Object    string `json:"object"`
	Bytes     int    `json:"bytes"`
	CreatedAt int64  `json:"created_at"`
	Filename  string `json:"filename"`
	Purpose   string `json:"purpose"`
}

// Represents the request body for creating a batch job.
type CreateBatchRequest struct {
	InputFileID      string `json:"input_file_id"`
	Endpoint         string `json:"endpoint"`
	CompletionWindow string `json:"completion_window"`
	// Optional metadata
	// Metadata map[string]string `json:"metadata,omitempty"`
}

// Represents the response from the batch creation endpoint.
type BatchResponse struct {
	ID string `json:"id"`
	// Add other fields as needed from the Batch object definition
	// e.g., Status, CreatedAt, etc.
	Object           string `json:"object"`
	Endpoint         string `json:"endpoint"`
	InputFileID      string `json:"input_file_id"`
	CompletionWindow string `json:"completion_window"`
	Status           string `json:"status"`
	CreatedAt        int64  `json:"created_at"`
}

const (
	openaiAPIKeyEnvVar = "OPENAI_API_KEY"
	openaiUploadURL    = "https://api.openai.com/v1/files"
	openaiBatchURL     = "https://api.openai.com/v1/batches"
	batchInputFileName = "batch_input.jsonl"
)

func main() {
	apiKey := os.Getenv(openaiAPIKeyEnvVar)
	if apiKey == "" {
		fmt.Printf("Error: %s environment variable not set.\n", openaiAPIKeyEnvVar)
		os.Exit(1)
	}

	// 1. Prepare the batch input data (JSONL format)
	requests := []BatchRequestItem{
		{
			CustomID: "request-1",
			Method:   "POST",
			URL:      "/v1/responses",
			Body: ResponseBody{
				Model: "gpt-4o-mini",
				Input: "System: You are a helpful assistant.\nUser: What is the capital of France?",
			},
		},
		{
			CustomID: "request-2",
			Method:   "POST",
			URL:      "/v1/responses",
			Body: ResponseBody{
				Model: "gpt-4o-mini",
				Input: "System: You are a helpful assistant.\nUser: Summarize the main plot of 'Hamlet'.",
			},
		},
		// Add more requests as needed
	}

	// 2. Create the batch input file (JSONL)
	err := createBatchInputFile(batchInputFileName, requests)
	if err != nil {
		fmt.Printf("Error creating batch input file: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(batchInputFileName) // Clean up the file afterwards
	fmt.Printf("Batch input file '%s' created successfully.\n", batchInputFileName)

	// 3. Upload the batch input file
	fileID, err := uploadFile(apiKey, batchInputFileName)
	if err != nil {
		fmt.Printf("Error uploading batch file: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("File uploaded successfully. File ID: %s\n", fileID)

	// 4. Create the batch job
	batchJob, err := createBatchJob(apiKey, fileID)
	if err != nil {
		fmt.Printf("Error creating batch job: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Batch job created successfully:\n%+v\n", batchJob)

	// 5. Monitor the batch job status
	fmt.Println("\nMonitoring batch job status...")
	err = monitorBatchJob(apiKey, batchJob.ID)
	if err != nil {
		fmt.Printf("Error monitoring batch job: %v\n", err)
		// Decide if os.Exit(1) is appropriate here based on monitoring failure
	}
}

// createBatchInputFile creates a JSONL file from the batch requests.
func createBatchInputFile(filename string, requests []BatchRequestItem) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	for _, req := range requests {
		if err := encoder.Encode(req); err != nil {
			return fmt.Errorf("failed to encode request %s: %w", req.CustomID, err)
		}
	}
	return nil
}

// uploadFile uploads the specified file to OpenAI for batch processing.
func uploadFile(apiKey, filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body) // Import "mime/multipart"

	// Add the purpose field
	purposeField, err := writer.CreateFormField("purpose")
	if err != nil {
		return "", fmt.Errorf("failed to create form field 'purpose': %w", err)
	}
	_, err = purposeField.Write([]byte("batch"))
	if err != nil {
		return "", fmt.Errorf("failed to write 'purpose' value: %w", err)
	}

	// Add the file field
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	_, err = io.Copy(part, file)
	if err != nil {
		return "", fmt.Errorf("failed to copy file content: %w", err)
	}

	err = writer.Close()
	if err != nil {
		return "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	req, err := http.NewRequest("POST", openaiUploadURL, body)
	if err != nil {
		return "", fmt.Errorf("failed to create upload request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute upload request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("upload failed with status %s: %s", resp.Status, string(respBody))
	}

	var uploadResp FileUploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
		return "", fmt.Errorf("failed to decode upload response: %w", err)
	}

	return uploadResp.ID, nil
}

// createBatchJob initiates the batch processing job on OpenAI.
func createBatchJob(apiKey, fileID string) (*BatchResponse, error) {
	batchReqPayload := CreateBatchRequest{
		InputFileID:      fileID,
		Endpoint:         "/v1/responses",
		CompletionWindow: "24h",
	}

	payloadBytes, err := json.Marshal(batchReqPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal batch request payload: %w", err)
	}

	req, err := http.NewRequest("POST", openaiBatchURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create batch job request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute batch job request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("batch job creation failed with status %s: %s", resp.Status, string(respBody))
	}

	var batchResp BatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&batchResp); err != nil {
		return nil, fmt.Errorf("failed to decode batch job response: %w", err)
	}

	return &batchResp, nil
}

// monitorBatchJob polls the batch job status until completion or timeout.
func monitorBatchJob(apiKey, batchID string) error {
	startTime := time.Now()
	timeout := 5 * time.Minute
	backoffDuration := 1 * time.Second
	maxBackoffDuration := 30 * time.Second

	client := &http.Client{Timeout: 20 * time.Second} // Shorter timeout for status checks

	for {
		// Check for overall timeout
		if time.Since(startTime) > timeout {
			return fmt.Errorf("timed out after %v waiting for batch job %s to complete", timeout, batchID)
		}

		statusURL := fmt.Sprintf("%s/%s", openaiBatchURL, batchID)
		req, err := http.NewRequest("GET", statusURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create status request for batch %s: %w", batchID, err)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)

		fmt.Printf("Checking status for batch %s...\n", batchID)
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("Error checking status for batch %s (will retry): %v\n", batchID, err)
			// Continue loop to retry after backoff
		} else {
			if resp.StatusCode != http.StatusOK {
				respBody, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				fmt.Printf("Error checking status for batch %s (HTTP %s, will retry): %s\n", batchID, resp.Status, string(respBody))
				// Continue loop to retry after backoff
			} else {
				var batchStatus BatchResponse
				if err := json.NewDecoder(resp.Body).Decode(&batchStatus); err != nil {
					resp.Body.Close()
					fmt.Printf("Error decoding status response for batch %s (will retry): %v\n", batchID, err)
					// Continue loop to retry after backoff
				} else {
					resp.Body.Close()
					fmt.Printf("Batch %s status: %s\n", batchID, batchStatus.Status)

					switch batchStatus.Status {
					case "completed", "succeeded": // Added "succeeded" for robustness
						fmt.Printf("Batch job %s completed successfully.\n", batchID)
						// Optionally: Add logic here to retrieve and process the results file
						// using batchStatus.OutputFileID or batchStatus.ErrorFileID
						return nil // Success
					case "failed", "expired", "cancelling", "cancelled":
						fmt.Printf("Batch job %s finished with terminal status: %s\n", batchID, batchStatus.Status)
						// Optionally: Check batchStatus.Errors for details
						return fmt.Errorf("batch job %s ended with status %s", batchID, batchStatus.Status)
					case "validating", "in_progress", "queued":
						// Continue polling
					default:
						fmt.Printf("Batch job %s has unknown status: %s\n", batchID, batchStatus.Status)
						// Continue polling, treat as in-progress
					}
					// Reset backoff on successful status check
					backoffDuration = 1 * time.Second
				}
			}
		}

		// Wait before next poll
		time.Sleep(backoffDuration)

		// Increase backoff duration for next time, up to max
		backoffDuration *= 2
		if backoffDuration > maxBackoffDuration {
			backoffDuration = maxBackoffDuration
		}
	}
}
