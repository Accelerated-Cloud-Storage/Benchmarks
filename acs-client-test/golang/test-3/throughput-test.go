// Copyright 2025 Accelerated Cloud Storage Corporation. All Rights Reserved.
package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"time"

	client "github.com/AcceleratedCloudStorage/acs-sdk-go/client"
)

func main() {
	// Initialize client
	acsClient, err := client.NewClient(&client.Session{
		Region: "us-east-1",
	})
	if err != nil {
		fmt.Printf("Failed to create client: %v\n", err)
		os.Exit(1)
	}
	defer acsClient.Close()

	// Create a unique bucket for testing
	bucket := fmt.Sprintf("throughput-test-%d", time.Now().UnixNano())
	ctx := context.Background()

	// Create bucket
	fmt.Printf("Creating bucket: %s\n", bucket)
	err = acsClient.CreateBucket(ctx, bucket)
	if err != nil {
		fmt.Printf("Failed to create bucket: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		fmt.Printf("Cleaning up bucket: %s\n", bucket)
		acsClient.DeleteBucket(ctx, bucket)
	}()

	// Define test object sizes (in bytes)
	objectSizes := []int{
		1 * 1024 * 1024,   // 1 MB
		10 * 1024 * 1024,  // 10 MB
		100 * 1024 * 1024, // 100 MB
		500 * 1024 * 1024, // 500 MB
	}
	numObjects := 50
	concurrency := 32 // Number of concurrent operations

	fmt.Println("\n--- THROUGHPUT BENCHMARK RESULTS ---")
	fmt.Println("Operation\tObject Size\tThroughput (ops/sec)\tThroughput (GB/sec)")
	fmt.Println("--------------------------------------------------------------------------------------------------------")

	// Step 1: Write objects of varying sizes
	fmt.Println("\nStarting concurrent write operations for varying object sizes...")
	for _, size := range objectSizes {
		fmt.Printf("Writing %d objects of size %d bytes with %d concurrent workers\n", numObjects, size, concurrency)
		startTime := time.Now()
		writeLatencies := runConcurrentOperations(ctx, acsClient, bucket, size, numObjects, concurrency, func(ctx context.Context, client *client.ACSClient, bucket, key string, data []byte) error {
			return client.PutObject(ctx, bucket, key, data)
		})
		calculateThroughput("PUT", size, writeLatencies, startTime)
	}

	// Step 2: Read objects
	fmt.Println("\nStarting concurrent read operations for varying object sizes...")
	for _, size := range objectSizes {
		fmt.Printf("Reading %d objects of size %d bytes with %d concurrent workers\n", numObjects, size, concurrency)
		startTime := time.Now()
		readLatencies := runConcurrentOperations(ctx, acsClient, bucket, size, numObjects, concurrency, func(ctx context.Context, client *client.ACSClient, bucket, key string, _ []byte) error {
			_, err := client.GetObject(ctx, bucket, key)
			return err
		})
		calculateThroughput("GET", size, readLatencies, startTime)
	}

	// Step 3: Delete all objects
	fmt.Println("\nStarting concurrent delete operations...")
	for _, size := range objectSizes {
		fmt.Printf("Deleting %d objects of size %d bytes with %d concurrent workers\n", numObjects, size, concurrency)
		startTime := time.Now()
		deleteLatencies := runConcurrentOperations(ctx, acsClient, bucket, size, numObjects, concurrency, func(ctx context.Context, client *client.ACSClient, bucket, key string, _ []byte) error {
			return client.DeleteObject(ctx, bucket, key)
		})
		calculateThroughput("DELETE", size, deleteLatencies, startTime)
	}

	fmt.Println("--------------------------------------------------------------------------------------------------------")
}

// Operation defines a generic storage operation function type
type Operation func(ctx context.Context, client *client.ACSClient, bucket, key string, data []byte) error

// runConcurrentOperations executes operations concurrently and returns latency measurements
func runConcurrentOperations(
	ctx context.Context,
	client *client.ACSClient,
	bucket string,
	objectSize int,
	numObjects int,
	concurrency int,
	op Operation,
) []time.Duration {
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, concurrency)
	results := make(chan time.Duration, numObjects)

	// Generate a sample data object (only used for write operations)
	sampleData := make([]byte, objectSize)
	rand.Read(sampleData)

	for i := 0; i < numObjects; i++ {
		key := fmt.Sprintf("key_%d_size_%d", i, objectSize)
		wg.Add(1)

		// Control concurrency with semaphore
		semaphore <- struct{}{}

		go func(objKey string) {
			defer func() {
				<-semaphore
				wg.Done()
			}()

			start := time.Now()
			err := op(ctx, client, bucket, objKey, sampleData)
			latency := time.Since(start)

			if err != nil {
				fmt.Printf("Operation failed for %s: %v\n", objKey, err)
				return
			}

			results <- latency
		}(key)
	}

	// Close results channel when all goroutines are done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect all latencies
	var latencies []time.Duration
	for latency := range results {
		latencies = append(latencies, latency)
	}

	return latencies
}

// calculateThroughput calculates and prints only throughput metrics
func calculateThroughput(operationType string, objectSize int, latencies []time.Duration, startTime time.Time) {
	successCount := len(latencies)
	if successCount == 0 {
		fmt.Printf("%s\t%d MB\tN/A (No successful operations)\tN/A\n",
			operationType, objectSize/(1024*1024))
		return
	}

	// Calculate total duration from the start to finish of all operations
	totalDuration := time.Since(startTime)

	// Calculate throughput metrics
	opsPerSec := float64(successCount) / totalDuration.Seconds()
	totalBytes := objectSize * successCount
	gbPerSec := (float64(totalBytes) / totalDuration.Seconds()) / (1024 * 1024 * 1024)

	// Print results in tabular format
	fmt.Printf("%s\t%d MB\t%.2f\t\t\t%.4f\n",
		operationType, objectSize/(1024*1024), opsPerSec, gbPerSec)
}
