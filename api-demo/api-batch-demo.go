package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/AcceleratedCloudStorage/acs-sdk-go/client"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go"
)

// Configuration constants
const (
	// Environment variables
	openaiAPIKeyEnvVar    = "OPENAI_API_KEY"
	anthropicAPIKeyEnvVar = "ANTHROPIC_API_KEY"

	// ACS configuration
	acsRegion = "us-east-1"

	// Model defaults
	defaultOpenAIModel    = "gpt-4o-mini"
	defaultAnthropicModel = "claude-3-5-haiku-20241022"

	// Batch processing configuration
	maxConcurrentRequests = 10
	batchTimeout          = 10 * time.Minute
	maxRetries            = 3
	retryBackoffInitial   = 1 * time.Second
	retryBackoffMax       = 30 * time.Second
)

// Error messages
const (
	errNoAPIKeys         = "no API keys found in environment variables"
	errACSClientCreation = "failed to create ACS client"
	errBucketCreation    = "failed to create ACS bucket"
	errBucketNameGen     = "failed to generate unique bucket name"
	errBatchProcessing   = "failed to process batch"
	errInvalidProvider   = "invalid provider type"
)

// ProviderType represents the LLM provider
type ProviderType string

const (
	ProviderOpenAI    ProviderType = "openai"
	ProviderAnthropic ProviderType = "anthropic"
)

// MessageRole represents the role of a message participant
type MessageRole string

const (
	RoleSystem    MessageRole = "system"
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
)

// Message represents a conversation message
type Message struct {
	Role    MessageRole `json:"role"`
	Content string      `json:"content"`
}

// BatchProcessor interface defines the contract for batch processing implementations
type BatchProcessor interface {
	ProcessBatch(ctx context.Context, items []GenericBatchItem) ([]BatchResponseItem, error)
}

// BatchConfig holds configuration for batch processing
type BatchConfig struct {
	MaxConcurrent int
	Timeout       time.Duration
	MaxRetries    int
}

// GenericBatchItem represents a provider-agnostic batch request item
type GenericBatchItem struct {
	ID        string            `json:"id"`
	Message   Message           `json:"message"`
	MaxTokens int               `json:"max_tokens"`
	Metadata  map[string]string `json:"metadata"`
}

// BatchResponseItem represents a provider-agnostic batch response
type BatchResponseItem struct {
	ID          string            `json:"id"`
	Content     string            `json:"content"`
	TokensUsed  int               `json:"tokens_used"`
	Timestamp   time.Time         `json:"timestamp"`
	Status      string            `json:"status"`
	ErrorDetail string            `json:"error_detail,omitempty"`
	Metadata    map[string]string `json:"metadata"`
}

// BatchStatus represents the status of a batch operation
type BatchStatus struct {
	BatchID     string              `json:"batch_id"`
	StartTime   time.Time           `json:"start_time"`
	LastUpdated time.Time           `json:"last_updated"`
	TotalItems  int                 `json:"total_items"`
	Completed   int                 `json:"completed"`
	Failed      int                 `json:"failed"`
	InProgress  int                 `json:"in_progress"`
	Status      string              `json:"status"` // "running", "completed", "failed"
	Responses   []BatchResponseItem `json:"responses"`
}

// BatchManager handles the orchestration of batch processing
type BatchManager struct {
	acsClient *client.ACSClient
	config    BatchConfig
	openAI    *openai.Client
	anthropic *anthropic.Client
	bucket    string
}

// NewBatchManager creates a new BatchManager instance
func NewBatchManager(ctx context.Context, config BatchConfig) (*BatchManager, error) {
	acsClient, err := client.NewClient(&client.Session{Region: acsRegion})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errACSClientCreation, err)
	}

	// Create a unique bucket for this batch manager
	bucket, err := generateUniqueBucketName()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errBucketNameGen, err)
	}

	if err := acsClient.CreateBucket(ctx, bucket); err != nil {
		return nil, fmt.Errorf("%s: %w", errBucketCreation, err)
	}

	openaiClient := openai.NewClient()
	anthropicClient := anthropic.NewClient()

	return &BatchManager{
		acsClient: acsClient,
		config:    config,
		openAI:    &openaiClient,
		anthropic: &anthropicClient,
		bucket:    bucket,
	}, nil
}

// Close cleans up resources used by BatchManager
func (bm *BatchManager) Close() error {
	ctx := context.Background()

	// List and delete all objects in the bucket
	opts := &client.ListObjectsOptions{Prefix: ""}
	keys, err := bm.acsClient.ListObjects(ctx, bm.bucket, opts)
	if err != nil {
		return fmt.Errorf("failed to list objects for cleanup: %w", err)
	}

	if len(keys) > 0 {
		if err := bm.acsClient.DeleteObjects(ctx, bm.bucket, keys); err != nil {
			return fmt.Errorf("failed to delete objects during cleanup: %w", err)
		}
	}

	// Delete the bucket
	if err := bm.acsClient.DeleteBucket(ctx, bm.bucket); err != nil {
		return fmt.Errorf("failed to delete bucket during cleanup: %w", err)
	}

	return nil
}

// StoreBatchItems stores batch items in ACS
func (bm *BatchManager) StoreBatchItems(ctx context.Context, items []GenericBatchItem) (string, error) {
	data, err := json.Marshal(items)
	if err != nil {
		return "", fmt.Errorf("failed to marshal batch items: %w", err)
	}

	key := fmt.Sprintf("batches/%d_items.json", time.Now().UnixNano())
	if err := bm.acsClient.PutObject(ctx, bm.bucket, key, data); err != nil {
		return "", fmt.Errorf("failed to store batch items: %w", err)
	}

	return key, nil
}

// LoadBatchItems loads batch items from ACS
func (bm *BatchManager) LoadBatchItems(ctx context.Context, key string) ([]GenericBatchItem, error) {
	data, err := bm.acsClient.GetObject(ctx, bm.bucket, key)
	if err != nil {
		return nil, fmt.Errorf("failed to load batch items: %w", err)
	}

	var items []GenericBatchItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("failed to unmarshal batch items: %w", err)
	}

	return items, nil
}

// StoreResponses stores batch responses in ACS
func (bm *BatchManager) StoreResponses(ctx context.Context, responses []BatchResponseItem) (string, error) {
	data, err := json.Marshal(responses)
	if err != nil {
		return "", fmt.Errorf("failed to marshal responses: %w", err)
	}

	key := fmt.Sprintf("responses/%d_results.json", time.Now().UnixNano())
	if err := bm.acsClient.PutObject(ctx, bm.bucket, key, data); err != nil {
		return "", fmt.Errorf("failed to store responses: %w", err)
	}

	return key, nil
}

// LoadResponses loads batch responses from ACS
func (bm *BatchManager) LoadResponses(ctx context.Context, key string) ([]BatchResponseItem, error) {
	data, err := bm.acsClient.GetObject(ctx, bm.bucket, key)
	if err != nil {
		return nil, fmt.Errorf("failed to load responses: %w", err)
	}

	var responses []BatchResponseItem
	if err := json.Unmarshal(data, &responses); err != nil {
		return nil, fmt.Errorf("failed to unmarshal responses: %w", err)
	}

	return responses, nil
}

// generateUniqueBucketName creates a unique bucket name for batch processing
func generateUniqueBucketName() (string, error) {
	// Generate a unique identifier using timestamp and random string
	timestamp := time.Now().UnixNano()
	randomBytes := make([]byte, 4)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	randomStr := hex.EncodeToString(randomBytes)

	return fmt.Sprintf("batch-%d-%s", timestamp, randomStr), nil
}

// determineProvider selects a provider based on batch analysis
func determineProvider(items []GenericBatchItem) ProviderType {
	openAIPreferred := 0
	anthropicPreferred := 0

	// Count provider preferences in metadata
	for _, item := range items {
		if provider, ok := item.Metadata["preferred_provider"]; ok {
			switch ProviderType(provider) {
			case ProviderOpenAI:
				openAIPreferred++
			case ProviderAnthropic:
				anthropicPreferred++
			}
		}
	}

	// If there's a clear preference in the batch, use that provider
	if openAIPreferred > anthropicPreferred {
		return ProviderOpenAI
	} else if anthropicPreferred > openAIPreferred {
		return ProviderAnthropic
	}

	// If no clear preference, check API key availability
	if os.Getenv(openaiAPIKeyEnvVar) != "" {
		return ProviderOpenAI
	}
	return ProviderAnthropic
}

// storeBatchStatus stores the current batch status in ACS
func (bm *BatchManager) storeBatchStatus(ctx context.Context, batchID string, status BatchStatus) error {
	data, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("failed to marshal batch status: %w", err)
	}

	key := fmt.Sprintf("batch-status/%s.json", batchID)
	if err := bm.acsClient.PutObject(ctx, bm.bucket, key, data); err != nil {
		return fmt.Errorf("failed to store batch status: %w", err)
	}

	return nil
}

// getBatchStatus retrieves the current batch status from ACS
func (bm *BatchManager) getBatchStatus(ctx context.Context, batchID string) (*BatchStatus, error) {
	key := fmt.Sprintf("batch-status/%s.json", batchID)
	data, err := bm.acsClient.GetObject(ctx, bm.bucket, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get batch status: %w", err)
	}

	var status BatchStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal batch status: %w", err)
	}

	return &status, nil
}

// BatchTask represents a single task in a batch request
type BatchTask struct {
	CustomID string                 `json:"custom_id"`
	Method   string                 `json:"method"`
	URL      string                 `json:"url"`
	Body     map[string]interface{} `json:"body"`
}

// ProcessBatch processes all items using a single provider
func (bm *BatchManager) ProcessBatch(ctx context.Context, items []GenericBatchItem) ([]BatchResponseItem, error) {
	if len(items) == 0 {
		return []BatchResponseItem{}, nil
	}

	// Generate a unique batch ID
	batchID := fmt.Sprintf("batch-%d", time.Now().UnixNano())

	// Initialize batch status
	status := BatchStatus{
		BatchID:     batchID,
		StartTime:   time.Now(),
		LastUpdated: time.Now(),
		TotalItems:  len(items),
		Status:      "running",
		InProgress:  len(items),
	}

	// Store initial status
	if err := bm.storeBatchStatus(ctx, batchID, status); err != nil {
		return nil, fmt.Errorf("failed to store initial batch status: %w", err)
	}

	// Determine single provider for entire batch
	provider := determineProvider(items)
	fmt.Printf("Using provider %s for batch %s with %d items\n", provider, batchID, len(items))

	// Process all items with the selected provider
	var responses []BatchResponseItem
	var err error

	switch provider {
	case ProviderOpenAI:
		responses, err = bm.processOpenAIItem(ctx, items)
	case ProviderAnthropic:
		responses, err = bm.processAnthropicItem(ctx, items)
	default:
		return nil, fmt.Errorf("%s: %s", errInvalidProvider, provider)
	}

	if err != nil {
		status.Status = "failed"
		status.Failed = len(items)
		status.InProgress = 0
	} else {
		status.Status = "completed"
		status.Completed = len(responses)
		status.InProgress = 0
	}

	// Store final status
	status.LastUpdated = time.Now()
	status.Responses = responses
	if err := bm.storeBatchStatus(ctx, batchID, status); err != nil {
		fmt.Printf("Failed to store final batch status: %v\n", err)
	}

	// Store final responses in a single file
	responsesKey := fmt.Sprintf("responses/%s/all_responses.json", batchID)
	responsesData, err := json.Marshal(responses)
	if err != nil {
		fmt.Printf("Failed to marshal final responses: %v\n", err)
	} else {
		if err := bm.acsClient.PutObject(ctx, bm.bucket, responsesKey, responsesData); err != nil {
			fmt.Printf("Failed to store final responses in ACS: %v\n", err)
		}
	}

	fmt.Printf("\nBatch processing completed: %d/%d items processed successfully\n",
		status.Completed, len(items))

	return responses, err
}

// processOpenAIItem processes all items using OpenAI's Batch API
func (bm *BatchManager) processOpenAIItem(ctx context.Context, items []GenericBatchItem) ([]BatchResponseItem, error) {
	// Create batch tasks for all items
	tasks := make([]map[string]interface{}, len(items))
	for i, item := range items {
		tasks[i] = map[string]interface{}{
			"custom_id": item.ID,
			"method":    "POST",
			"url":       "/v1/chat/completions",
			"body": map[string]interface{}{
				"model":      defaultOpenAIModel,
				"messages":   []map[string]interface{}{{"role": string(item.Message.Role), "content": item.Message.Content}},
				"max_tokens": item.MaxTokens,
			},
		}
	}

	// Create a temporary JSONL file
	tmpFile, err := os.CreateTemp("", "batch-*.jsonl")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write all tasks as JSONL
	encoder := json.NewEncoder(tmpFile)
	for _, task := range tasks {
		if err := encoder.Encode(task); err != nil {
			tmpFile.Close()
			return nil, fmt.Errorf("failed to write batch task: %w", err)
		}
	}
	tmpFile.Close()

	// Reopen the file for reading
	tmpFile, err = os.Open(tmpFile.Name())
	if err != nil {
		return nil, fmt.Errorf("failed to open batch file: %w", err)
	}
	defer tmpFile.Close()

	fmt.Printf("Creating batch file for %d requests...\n", len(items))
	batchFile, err := bm.openAI.Files.New(ctx, openai.FileNewParams{
		File:    tmpFile,
		Purpose: "batch",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create batch file: %w", err)
	}

	fmt.Printf("Creating batch job for %d requests...\n", len(items))
	batchJob, err := bm.openAI.Batches.New(ctx, openai.BatchNewParams{
		InputFileID:      batchFile.ID,
		Endpoint:         "/v1/chat/completions",
		CompletionWindow: openai.BatchNewParamsCompletionWindow24h,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create batch job: %w", err)
	}

	fmt.Printf("Batch job %s created, waiting for completion...\n", batchJob.ID)

	// Poll for completion with exponential backoff
	backoff := time.Second * 10
	maxBackoff := time.Minute * 5
	deadline := time.Now().Add(batchTimeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
			job, err := bm.openAI.Batches.Get(ctx, batchJob.ID)
			if err != nil {
				return nil, fmt.Errorf("failed to get batch job status: %w", err)
			}

			fmt.Printf("Batch job %s status: %s\n", batchJob.ID, job.Status)

			switch job.Status {
			case openai.BatchStatusCompleted:
				fmt.Printf("Batch job %s completed, retrieving results...\n", batchJob.ID)
				resultContent, err := bm.openAI.Files.Content(ctx, job.OutputFileID)
				if err != nil {
					return nil, fmt.Errorf("failed to get batch results: %w", err)
				}
				defer resultContent.Body.Close()

				// Read the entire response body
				resultBytes, err := io.ReadAll(resultContent.Body)
				if err != nil {
					return nil, fmt.Errorf("failed to read result content: %w", err)
				}

				// Process results - the response is a JSONL file
				responses := make([]BatchResponseItem, 0, len(items))
				scanner := bufio.NewScanner(bytes.NewReader(resultBytes))
				for scanner.Scan() {
					var result struct {
						CustomID string `json:"custom_id"`
						Response struct {
							Body struct {
								Choices []struct {
									Message struct {
										Content string `json:"content"`
									} `json:"message"`
								} `json:"choices"`
								Usage struct {
									TotalTokens int `json:"total_tokens"`
								} `json:"usage"`
							} `json:"body"`
						} `json:"response"`
					}

					if err := json.Unmarshal(scanner.Bytes(), &result); err != nil {
						return nil, fmt.Errorf("failed to decode batch result line: %w", err)
					}

					// Find the corresponding item
					var item GenericBatchItem
					for _, i := range items {
						if i.ID == result.CustomID {
							item = i
							break
						}
					}

					response := BatchResponseItem{
						ID:         result.CustomID,
						Timestamp:  time.Now(),
						Status:     "success",
						Metadata:   item.Metadata,
						TokensUsed: result.Response.Body.Usage.TotalTokens,
					}

					if len(result.Response.Body.Choices) > 0 {
						response.Content = result.Response.Body.Choices[0].Message.Content
					}

					responses = append(responses, response)
					fmt.Printf("Successfully processed request %s\n", result.CustomID)
				}

				if err := scanner.Err(); err != nil {
					return nil, fmt.Errorf("error scanning batch results: %w", err)
				}

				return responses, nil

			case openai.BatchStatusFailed:
				return nil, fmt.Errorf("batch job failed")

			case openai.BatchStatusExpired, openai.BatchStatusCancelled:
				return nil, fmt.Errorf("batch job %s", job.Status)

			case openai.BatchStatusInProgress, openai.BatchStatusValidating, openai.BatchStatusFinalizing:
				// Job still processing, continue polling
				backoff = min(backoff*2, maxBackoff)
				continue

			default:
				return nil, fmt.Errorf("unexpected batch job status: %s", job.Status)
			}
		}
	}

	return nil, fmt.Errorf("batch job timed out after %v", batchTimeout)
}

// processAnthropicItem processes all items using Anthropic's Batch API
func (bm *BatchManager) processAnthropicItem(ctx context.Context, items []GenericBatchItem) ([]BatchResponseItem, error) {
	// Convert all items to Anthropic format
	requests := make([]anthropic.BetaMessageBatchNewParamsRequest, len(items))
	for i, item := range items {
		// For each item, we need to ensure we have a proper message structure
		var messages []anthropic.BetaMessageParam
		var systemPrompt string

		// Handle the message based on its role
		switch item.Message.Role {
		case RoleSystem:
			// For system messages, we store it as system prompt
			systemPrompt = item.Message.Content
			// Add a default user message to ensure the conversation starts
			messages = append(messages, anthropic.BetaMessageParam{
				Role: anthropic.BetaMessageParamRoleUser,
				Content: []anthropic.BetaContentBlockParamUnion{{
					OfRequestTextBlock: &anthropic.BetaTextBlockParam{
						Text: "Please help me.",
					},
				}},
			})
		case RoleUser:
			messages = append(messages, anthropic.BetaMessageParam{
				Role: anthropic.BetaMessageParamRoleUser,
				Content: []anthropic.BetaContentBlockParamUnion{{
					OfRequestTextBlock: &anthropic.BetaTextBlockParam{
						Text: item.Message.Content,
					},
				}},
			})
		case RoleAssistant:
			messages = append(messages, anthropic.BetaMessageParam{
				Role: anthropic.BetaMessageParamRoleAssistant,
				Content: []anthropic.BetaContentBlockParamUnion{{
					OfRequestTextBlock: &anthropic.BetaTextBlockParam{
						Text: item.Message.Content,
					},
				}},
			})
		}

		// Create the batch request
		request := anthropic.BetaMessageBatchNewParamsRequest{
			CustomID: item.ID,
			Params: anthropic.BetaMessageBatchNewParamsRequestParams{
				Model:     defaultAnthropicModel,
				MaxTokens: int64(item.MaxTokens),
				Messages:  messages,
			},
		}

		// Add system prompt if present
		if systemPrompt != "" {
			request.Params.System = []anthropic.BetaTextBlockParam{{
				Text: systemPrompt,
			}}
		}

		requests[i] = request
	}

	fmt.Printf("Creating batch job for %d requests...\n", len(items))
	batch, err := bm.anthropic.Beta.Messages.Batches.New(ctx, anthropic.BetaMessageBatchNewParams{
		Requests: requests,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create batch: %w", err)
	}

	fmt.Printf("Batch job %s created, waiting for completion...\n", batch.ID)

	// Poll for completion with exponential backoff
	backoff := time.Second * 10
	maxBackoff := time.Minute * 5
	deadline := time.Now().Add(batchTimeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
			batch, err = bm.anthropic.Beta.Messages.Batches.Get(ctx, batch.ID, anthropic.BetaMessageBatchGetParams{})
			if err != nil {
				return nil, fmt.Errorf("failed to get batch status: %w", err)
			}

			fmt.Printf("Batch job %s status: %s\n", batch.ID, batch.ProcessingStatus)

			if batch.ProcessingStatus == anthropic.BetaMessageBatchProcessingStatusEnded {
				fmt.Printf("Batch job %s completed, retrieving results...\n", batch.ID)

				// Get results using streaming
				stream := bm.anthropic.Beta.Messages.Batches.ResultsStreaming(ctx, batch.ID, anthropic.BetaMessageBatchResultsParams{})
				if stream.Err() != nil {
					return nil, fmt.Errorf("failed to get batch results: %w", stream.Err())
				}

				responses := make([]BatchResponseItem, 0, len(items))
				for stream.Next() {
					result := stream.Current()

					// Find the corresponding item
					var item GenericBatchItem
					for _, i := range items {
						if i.ID == result.CustomID {
							item = i
							break
						}
					}

					response := BatchResponseItem{
						ID:        result.CustomID,
						Timestamp: time.Now(),
						Metadata:  item.Metadata,
					}

					switch variant := result.Result.AsAny().(type) {
					case anthropic.BetaMessageBatchSucceededResult:
						response.Status = "success"
						if len(variant.Message.Content) > 0 {
							response.Content = variant.Message.Content[0].Text
						}
						response.TokensUsed = int(variant.Message.Usage.InputTokens + variant.Message.Usage.OutputTokens)
						fmt.Printf("Successfully processed request %s\n", result.CustomID)

					case anthropic.BetaMessageBatchErroredResult:
						response.Status = "failed"
						response.ErrorDetail = string(variant.Error.Type)
						fmt.Printf("Failed to process request %s: %s\n", result.CustomID, variant.Error.Type)

					case anthropic.BetaMessageBatchCanceledResult:
						response.Status = "failed"
						response.ErrorDetail = "Request was canceled"
						fmt.Printf("Request %s was canceled\n", result.CustomID)

					case anthropic.BetaMessageBatchExpiredResult:
						response.Status = "failed"
						response.ErrorDetail = "Request expired"
						fmt.Printf("Request %s expired\n", result.CustomID)
					}

					responses = append(responses, response)
				}

				if err := stream.Err(); err != nil {
					return nil, fmt.Errorf("error streaming batch results: %w", err)
				}

				return responses, nil
			}

			// Exponential backoff for polling
			backoff = min(backoff*2, maxBackoff)
		}
	}

	return nil, fmt.Errorf("batch job timed out after %v", batchTimeout)
}

// ListBatchRequests lists all batch requests stored in ACS with the given prefix
func (bm *BatchManager) ListBatchRequests(ctx context.Context, prefix string) ([]string, error) {
	opts := &client.ListObjectsOptions{
		Prefix: fmt.Sprintf("batches/%s", prefix),
	}
	return bm.acsClient.ListObjects(ctx, bm.bucket, opts)
}

// AddBatchRequests adds new items to an existing batch
func (bm *BatchManager) AddBatchRequests(ctx context.Context, batchKey string, newItems []GenericBatchItem) error {
	// Load existing items
	existingItems, err := bm.LoadBatchItems(ctx, batchKey)
	if err != nil {
		return fmt.Errorf("failed to load existing batch items: %w", err)
	}

	// Add new items
	existingItems = append(existingItems, newItems...)

	// Store updated batch
	data, err := json.Marshal(existingItems)
	if err != nil {
		return fmt.Errorf("failed to marshal updated batch items: %w", err)
	}

	return bm.acsClient.PutObject(ctx, bm.bucket, batchKey, data)
}

// RemoveBatchRequests removes items from a batch by their IDs
func (bm *BatchManager) RemoveBatchRequests(ctx context.Context, batchKey string, itemIDs []string) error {
	// Load existing items
	items, err := bm.LoadBatchItems(ctx, batchKey)
	if err != nil {
		return fmt.Errorf("failed to load batch items: %w", err)
	}

	// Create a map of IDs to remove for O(1) lookup
	removeIDs := make(map[string]bool)
	for _, id := range itemIDs {
		removeIDs[id] = true
	}

	// Filter out items to remove
	filteredItems := make([]GenericBatchItem, 0, len(items))
	for _, item := range items {
		if !removeIDs[item.ID] {
			filteredItems = append(filteredItems, item)
		}
	}

	// Store updated batch
	data, err := json.Marshal(filteredItems)
	if err != nil {
		return fmt.Errorf("failed to marshal updated batch items: %w", err)
	}

	return bm.acsClient.PutObject(ctx, bm.bucket, batchKey, data)
}

// GetBatchProgress returns the current progress of a batch operation
func (bm *BatchManager) GetBatchProgress(ctx context.Context, batchID string) (*BatchStatus, error) {
	return bm.getBatchStatus(ctx, batchID)
}

// ListBatches returns a list of all batch IDs
func (bm *BatchManager) ListBatches(ctx context.Context) ([]string, error) {
	opts := &client.ListObjectsOptions{
		Prefix: "batch-status/",
	}

	keys, err := bm.acsClient.ListObjects(ctx, bm.bucket, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to list batches: %w", err)
	}

	// Extract batch IDs from keys
	batchIDs := make([]string, 0, len(keys))
	for _, key := range keys {
		// Remove prefix and .json suffix
		batchID := strings.TrimPrefix(key, "batch-status/")
		batchID = strings.TrimSuffix(batchID, ".json")
		batchIDs = append(batchIDs, batchID)
	}

	return batchIDs, nil
}

func main() {
	ctx := context.Background()

	// Verify API keys are present
	_, hasOpenAI := os.LookupEnv(openaiAPIKeyEnvVar)
	_, hasAnthropic := os.LookupEnv(anthropicAPIKeyEnvVar)

	if !hasOpenAI && !hasAnthropic {
		fmt.Printf("Error: %s\n", errNoAPIKeys)
		fmt.Printf("Please set at least one of these environment variables:\n")
		fmt.Printf("  - %s (for OpenAI)\n", openaiAPIKeyEnvVar)
		fmt.Printf("  - %s (for Anthropic)\n", anthropicAPIKeyEnvVar)
		os.Exit(1)
	}

	// Initialize batch manager
	batchManager, err := NewBatchManager(ctx, BatchConfig{
		MaxConcurrent: maxConcurrentRequests,
		Timeout:       batchTimeout,
		MaxRetries:    maxRetries,
	})
	if err != nil {
		fmt.Printf("Error initializing batch manager: %v\n", err)
		os.Exit(1)
	}
	defer batchManager.Close()

	// Create initial batch items
	initialBatchItems := []GenericBatchItem{
		{
			ID: "request-1",
			Message: Message{
				Role:    RoleSystem,
				Content: "You are a helpful assistant.",
			},
			MaxTokens: 100,
			Metadata: map[string]string{
				"category":           "geography",
				"preferred_provider": string(ProviderOpenAI),
			},
		},
		{
			ID: "request-2",
			Message: Message{
				Role:    RoleUser,
				Content: "What is the capital of France?",
			},
			MaxTokens: 100,
			Metadata: map[string]string{
				"category":           "geography",
				"preferred_provider": string(ProviderOpenAI),
			},
		},
	}

	// Store initial batch
	batchKey, err := batchManager.StoreBatchItems(ctx, initialBatchItems)
	if err != nil {
		fmt.Printf("Error storing initial batch: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Initial batch stored with key: %s\n", batchKey)

	// Add more items to the batch
	additionalItems := []GenericBatchItem{
		{
			ID: "request-3",
			Message: Message{
				Role:    RoleSystem,
				Content: "You are a helpful assistant.",
			},
			MaxTokens: 150,
			Metadata: map[string]string{
				"category":           "literature",
				"preferred_provider": string(ProviderAnthropic),
			},
		},
	}

	err = batchManager.AddBatchRequests(ctx, batchKey, additionalItems)
	if err != nil {
		fmt.Printf("Error adding items to batch: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Added additional items to batch\n")

	// List all batch requests
	batches, err := batchManager.ListBatchRequests(ctx, "")
	if err != nil {
		fmt.Printf("Error listing batches: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\nStored batches:\n")
	for _, batch := range batches {
		fmt.Printf("  - %s\n", batch)
	}

	// Load and display current batch contents
	currentBatch, err := batchManager.LoadBatchItems(ctx, batchKey)
	if err != nil {
		fmt.Printf("Error loading current batch: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\nCurrent batch contents (%d items):\n", len(currentBatch))
	for _, item := range currentBatch {
		fmt.Printf("  - ID: %s, Provider: %s\n", item.ID, item.Metadata["preferred_provider"])
	}

	// Remove an item from the batch
	err = batchManager.RemoveBatchRequests(ctx, batchKey, []string{"request-1"})
	if err != nil {
		fmt.Printf("Error removing items from batch: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\nRemoved request-1 from batch\n")

	// Load final batch before processing
	finalBatch, err := batchManager.LoadBatchItems(ctx, batchKey)
	if err != nil {
		fmt.Printf("Error loading final batch: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\nFinal batch contents (%d items):\n", len(finalBatch))
	for _, item := range finalBatch {
		fmt.Printf("  - ID: %s, Provider: %s\n", item.ID, item.Metadata["preferred_provider"])
	}

	// Process final batch
	responses, err := batchManager.ProcessBatch(ctx, finalBatch)
	if err != nil {
		fmt.Printf("Error processing batch: %v\n", err)
		os.Exit(1)
	}

	// Print results
	fmt.Printf("\n=== Batch Results ===\n")
	printResponseSummary(responses)
}

// printResponseSummary prints a summary of batch responses
func printResponseSummary(responses []BatchResponseItem) {
	// Count responses by status
	stats := make(map[string]int)
	for _, resp := range responses {
		stats[resp.Status]++
	}

	// Print stats
	fmt.Println("\nResponse Statistics:")
	if successCount := stats["success"]; successCount > 0 {
		fmt.Printf("  %d succeeded", successCount)
	}
	if errorCount := stats["failed"]; errorCount > 0 {
		fmt.Printf(", %d failed", errorCount)
	}
	fmt.Println()

	// Print detailed responses
	fmt.Println("\nDetailed Responses:")
	for i, resp := range responses {
		fmt.Printf("  %d. [%s] Request: %s\n", i+1, resp.Status, resp.ID)

		if resp.Status == "success" {
			// Truncate content if too long
			content := resp.Content
			if len(content) > 100 {
				content = content[:97] + "..."
			}
			fmt.Printf("     Response: %s\n", content)
			fmt.Printf("     Tokens: %d\n", resp.TokensUsed)
		} else {
			fmt.Printf("     Error: %s\n", resp.ErrorDetail)
		}

		// Add extra line between responses
		fmt.Println()
	}
}
