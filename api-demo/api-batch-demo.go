package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/AcceleratedCloudStorage/acs-sdk-go/client"
)

// Represents a single request within the batch file (JSONL format).
type BatchRequestItem struct {
	CustomID string `json:"custom_id"`
	Method   string `json:"method"`
	URL      string `json:"url"`
	Body     any    `json:"body"`
}

// Represents the body for a /v1/responses request.
type ResponseBody struct {
	Model string `json:"model"`
	Input string `json:"input"`
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

	// ACS constants
	acsRegion = "us-east-1"
)

// generateUniqueBucketName creates a unique bucket name using a timestamp and random string
func generateUniqueBucketName() (string, error) {
	// Generate 8 random bytes
	randomBytes := make([]byte, 8)
	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// Convert to base64 and make valid bucket name (lowercase, no special chars)
	randStr := strings.ToLower(base64.RawURLEncoding.EncodeToString(randomBytes))

	// Create the bucket name with timestamp prefix
	bucketName := fmt.Sprintf("openai-batch-%d-%s", time.Now().Unix(), randStr)

	// Ensure bucket name conforms to S3 naming rules
	// - lowercase only
	// - 3-63 characters
	// - no uppercase, underscores, or special characters except hyphen
	bucketName = strings.ToLower(bucketName)
	if len(bucketName) > 63 {
		bucketName = bucketName[:63]
	}

	return bucketName, nil
}

func main() {
	apiKey := os.Getenv(openaiAPIKeyEnvVar)
	if apiKey == "" {
		fmt.Printf("Error: %s environment variable not set.\n", openaiAPIKeyEnvVar)
		os.Exit(1)
	}

	// Create ACS client
	ctx := context.Background()
	acsClient, err := client.NewClient(&client.Session{Region: acsRegion})
	if err != nil {
		fmt.Printf("Error creating ACS client: %v\n", err)
		os.Exit(1)
	}
	defer acsClient.Close()

	// Generate a unique bucket name
	bucketName, err := generateUniqueBucketName()
	if err != nil {
		fmt.Printf("Error generating unique bucket name: %v\n", err)
		os.Exit(1)
	}

	// Create the new bucket
	fmt.Printf("Creating new ACS bucket: %s\n", bucketName)
	err = acsClient.CreateBucket(ctx, bucketName)
	if err != nil {
		fmt.Printf("Error creating ACS bucket: %v\n", err)
		os.Exit(1)
	}

	// Schedule bucket cleanup for when we're done
	defer func() {
		fmt.Printf("Cleaning up ACS bucket: %s\n", bucketName)

		// First list and delete all objects in the bucket
		listOpts := &client.ListObjectsOptions{Prefix: ""}
		keys, err := acsClient.ListObjects(ctx, bucketName, listOpts)
		if err != nil {
			fmt.Printf("Warning: Failed to list objects in bucket: %v\n", err)
		} else if len(keys) > 0 {
			// Delete all objects
			fmt.Printf("Deleting %d objects from bucket...\n", len(keys))
			err = acsClient.DeleteObjects(ctx, bucketName, keys)
			if err != nil {
				fmt.Printf("Warning: Failed to delete objects: %v\n", err)
			}
		}

		// Now delete the bucket itself
		err = acsClient.DeleteBucket(ctx, bucketName)
		if err != nil {
			fmt.Printf("Warning: Failed to delete bucket: %v\n", err)
		} else {
			fmt.Printf("Successfully deleted bucket: %s\n", bucketName)
		}
	}()

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

	// Generate a unique key for storing in ACS
	acsKey := fmt.Sprintf("batch_files/%d_%s", time.Now().Unix(), batchInputFileName)

	// 2. Create and upload batch file to ACS
	err = createAndUploadBatchToACS(ctx, acsClient, bucketName, acsKey, requests)
	if err != nil {
		fmt.Printf("Error creating and uploading batch file to ACS: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Batch file uploaded to ACS successfully with key: %s\n", acsKey)

	// 3. Demonstrate batch file operations
	fmt.Println("\n=== Demonstrating batch file operations ===")

	// 3.1 List the current batch contents
	fmt.Println("\nListing initial batch contents:")
	batchItems, err := listBatchContents(ctx, acsClient, bucketName, acsKey)
	if err != nil {
		fmt.Printf("Error listing batch contents: %v\n", err)
		os.Exit(1)
	}
	printBatchSummary(batchItems)

	// 3.2 Add a new request to the batch
	newRequest := BatchRequestItem{
		CustomID: "request-3",
		Method:   "POST",
		URL:      "/v1/responses",
		Body: ResponseBody{
			Model: "gpt-4o-mini",
			Input: "System: You are a helpful assistant.\nUser: Explain quantum computing in simple terms.",
		},
	}

	fmt.Println("\nAdding new request to batch file...")
	err = addRequestToBatch(ctx, acsClient, bucketName, acsKey, newRequest)
	if err != nil {
		fmt.Printf("Error adding request to batch: %v\n", err)
		os.Exit(1)
	}

	// 3.3 List the updated batch contents
	fmt.Println("\nListing batch contents after addition:")
	batchItems, err = listBatchContents(ctx, acsClient, bucketName, acsKey)
	if err != nil {
		fmt.Printf("Error listing batch contents: %v\n", err)
		os.Exit(1)
	}
	printBatchSummary(batchItems)

	// 3.4 Remove a request from the batch
	requestToRemove := "request-1"
	fmt.Printf("\nRemoving request with CustomID '%s' from batch file...\n", requestToRemove)
	err = removeRequestFromBatch(ctx, acsClient, bucketName, acsKey, requestToRemove)
	if err != nil {
		fmt.Printf("Error removing request from batch: %v\n", err)
		os.Exit(1)
	}

	// 3.5 List the final batch contents
	fmt.Println("\nListing batch contents after removal:")
	batchItems, err = listBatchContents(ctx, acsClient, bucketName, acsKey)
	if err != nil {
		fmt.Printf("Error listing batch contents: %v\n", err)
		os.Exit(1)
	}
	printBatchSummary(batchItems)

	// 4. Download the file from ACS for processing
	batchData, err := downloadBatchFromACS(ctx, acsClient, bucketName, acsKey)
	if err != nil {
		fmt.Printf("Error downloading batch file from ACS: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\nBatch file downloaded from ACS successfully (%d bytes)\n", len(batchData))

	// 5. Upload the batch file to OpenAI for processing
	fileID, err := uploadBatchDataToOpenAI(apiKey, batchInputFileName, batchData)
	if err != nil {
		fmt.Printf("Error uploading batch file to OpenAI: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("File uploaded to OpenAI successfully. File ID: %s\n", fileID)

	// 6. Create the batch job
	batchJob, err := createBatchJob(apiKey, fileID)
	if err != nil {
		fmt.Printf("Error creating batch job: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Batch job created successfully:\n%+v\n", batchJob)

	// 7. Monitor the batch job status
	fmt.Println("\nMonitoring batch job status...")
	err = monitorBatchJob(apiKey, batchJob.ID)
	if err != nil {
		fmt.Printf("Error monitoring batch job: %v\n", err)
		// We don't exit here to ensure bucket cleanup happens
	}

	fmt.Println("All operations completed. Cleaning up resources...")
}

// listBatchContents retrieves and parses the batch file to return its contents
func listBatchContents(ctx context.Context, acsClient *client.ACSClient, bucketName, key string) ([]BatchRequestItem, error) {
	// Download the batch file data
	data, err := acsClient.GetObject(ctx, bucketName, key)
	if err != nil {
		return nil, fmt.Errorf("failed to download batch file: %w", err)
	}

	// Parse the JSONL format
	var requests []BatchRequestItem
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue // Skip empty lines
		}

		var request BatchRequestItem
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			return nil, fmt.Errorf("failed to parse batch request: %w", err)
		}
		requests = append(requests, request)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning batch file: %w", err)
	}

	return requests, nil
}

// addRequestToBatch adds a new request to an existing batch file
func addRequestToBatch(ctx context.Context, acsClient *client.ACSClient, bucketName, key string, newRequest BatchRequestItem) error {
	// Get existing requests
	requests, err := listBatchContents(ctx, acsClient, bucketName, key)
	if err != nil {
		return err
	}

	// Check for duplicate CustomID
	for _, req := range requests {
		if req.CustomID == newRequest.CustomID {
			return fmt.Errorf("a request with CustomID '%s' already exists in the batch", newRequest.CustomID)
		}
	}

	// Add the new request
	requests = append(requests, newRequest)

	// Upload the updated batch
	return createAndUploadBatchToACS(ctx, acsClient, bucketName, key, requests)
}

// removeRequestFromBatch removes a request from an existing batch file by its CustomID
func removeRequestFromBatch(ctx context.Context, acsClient *client.ACSClient, bucketName, key string, customID string) error {
	// Get existing requests
	requests, err := listBatchContents(ctx, acsClient, bucketName, key)
	if err != nil {
		return err
	}

	// Find and remove the request with the matching CustomID
	found := false
	var updatedRequests []BatchRequestItem
	for _, req := range requests {
		if req.CustomID == customID {
			found = true
			continue // Skip this request (remove it)
		}
		updatedRequests = append(updatedRequests, req)
	}

	if !found {
		return fmt.Errorf("no request with CustomID '%s' found in the batch", customID)
	}

	// Check if the batch would be empty
	if len(updatedRequests) == 0 {
		return fmt.Errorf("cannot remove the only request in the batch; batch must contain at least one request")
	}

	// Upload the updated batch
	return createAndUploadBatchToACS(ctx, acsClient, bucketName, key, updatedRequests)
}

// printBatchSummary displays a summary of the batch contents
func printBatchSummary(items []BatchRequestItem) {
	fmt.Printf("Total requests in batch: %d\n", len(items))
	for i, item := range items {
		// Try to extract the prompt from different types of Body
		var prompt string
		if respBody, ok := item.Body.(map[string]interface{}); ok {
			if input, ok := respBody["input"].(string); ok {
				// Truncate long inputs
				if len(input) > 60 {
					prompt = input[:57] + "..."
				} else {
					prompt = input
				}
			}
		}

		fmt.Printf("  %d. CustomID: %s, Endpoint: %s, Prompt: %s\n",
			i+1, item.CustomID, item.URL, prompt)
	}
}

// createAndUploadBatchToACS creates a JSONL file from batch requests and uploads it to ACS
func createAndUploadBatchToACS(ctx context.Context, acsClient *client.ACSClient, bucketName, key string, requests []BatchRequestItem) error {
	// Create JSONL data in memory
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	for _, req := range requests {
		if err := encoder.Encode(req); err != nil {
			return fmt.Errorf("failed to encode request %s: %w", req.CustomID, err)
		}
	}

	// Upload data to ACS
	if err := acsClient.PutObject(ctx, bucketName, key, buf.Bytes()); err != nil {
		return fmt.Errorf("failed to upload batch file to ACS: %w", err)
	}

	return nil
}

// downloadBatchFromACS downloads a batch file from ACS
func downloadBatchFromACS(ctx context.Context, acsClient *client.ACSClient, bucketName, key string) ([]byte, error) {
	data, err := acsClient.GetObject(ctx, bucketName, key)
	if err != nil {
		return nil, fmt.Errorf("failed to download batch file from ACS: %w", err)
	}
	return data, nil
}

// uploadBatchDataToOpenAI uploads the provided data to OpenAI's Files API
func uploadBatchDataToOpenAI(apiKey, filename string, data []byte) (string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

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
	_, err = part.Write(data)
	if err != nil {
		return "", fmt.Errorf("failed to write file data: %w", err)
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
