package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func main() {
	// Initialize client
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion("us-east-1"))
	if err != nil {
		fmt.Printf("Failed to load AWS config: %v\n", err)
		os.Exit(1)
	}
	// Initialize client with Tigris endpoint
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String("https://fly.storage.tigris.dev")
		o.Region = "auto"
		o.UsePathStyle = false
	})

	// Create a unique bucket for testing
	bucket := fmt.Sprintf("test-bucket-%d", time.Now().UnixNano())
	ctx := context.Background()

	// Create bucket
	fmt.Printf("Creating bucket: %s\n", bucket)
	_, err = client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		fmt.Printf("Failed to create bucket: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		fmt.Printf("Cleaning up bucket: %s\n", bucket)
		// Delete all objects before deleting bucket
		listObjectsResp, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: aws.String(bucket),
		})
		if err != nil {
			fmt.Printf("Failed to list objects: %v\n", err)
			return
		}

		for _, obj := range listObjectsResp.Contents {
			_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(bucket),
				Key:    obj.Key,
			})
			if err != nil {
				fmt.Printf("Failed to delete object %s: %v\n", *obj.Key, err)
			}
		}

		_, err = client.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: aws.String(bucket),
		})
		if err != nil {
			fmt.Printf("Failed to delete bucket: %v\n", err)
		}
	}()
	
	// Define test object sizes
	objectSizes := []int{1024, 1024 * 1024, 10 * 1024 * 1024} // 1KB, 1MB, 10MB
	numObjects := 50

	// Step 1: Write objects of varying sizes
	fmt.Println("Starting write operations for varying object sizes...")
	for _, size := range objectSizes {
		fmt.Printf("\nWriting %d objects of size %d bytes\n", numObjects, size)
		writeLatencies := make([]time.Duration, numObjects)

		for i := 0; i < numObjects; i++ {
			key := fmt.Sprintf("key_%d_size_%d", i, size)
			data := make([]byte, size)
			rand.Read(data)

			start := time.Now()
			_, err := client.PutObject(ctx, &s3.PutObjectInput{
				Bucket: aws.String(bucket),
				Key:    aws.String(key),
				Body:   bytes.NewReader(data),
			})
			writeLatencies[i] = time.Since(start)

			if err != nil {
				fmt.Printf("Failed to put object: %v\n", err)
				continue
			}
		}
		calculateMetrics(writeLatencies, fmt.Sprintf("Write (Size: %d bytes)", size), size)
	}

	// Step 2: Read objects
	fmt.Println("\nStarting read operations for varying object sizes...")
	for _, size := range objectSizes {
		fmt.Printf("\nReading %d objects of size %d bytes\n", numObjects, size)
		readLatencies := make([]time.Duration, numObjects)

		for i := 0; i < numObjects; i++ {
			key := fmt.Sprintf("key_%d_size_%d", i, size)

			start := time.Now()
			resp, err := client.GetObject(ctx, &s3.GetObjectInput{
				Bucket: aws.String(bucket),
				Key:    aws.String(key),
			})

			if err == nil {
				// Consume the response to fully complete the operation
				_, _ = io.ReadAll(resp.Body)
				resp.Body.Close()
			}

			readLatencies[i] = time.Since(start)

			if err != nil {
				fmt.Printf("Failed to get object: %v\n", err)
				continue
			}
		}
		calculateMetrics(readLatencies, fmt.Sprintf("Read (Size: %d bytes)", size), size)
	}

	// Step 3: Delete all objects
	fmt.Println("\nStarting delete operations...")
	for _, size := range objectSizes {
		fmt.Printf("\nDeleting %d objects of size %d bytes\n", numObjects, size)
		deleteLatencies := make([]time.Duration, numObjects)

		for i := 0; i < numObjects; i++ {
			key := fmt.Sprintf("key_%d_size_%d", i, size)

			start := time.Now()
			_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(bucket),
				Key:    aws.String(key),
			})
			deleteLatencies[i] = time.Since(start)

			if err != nil {
				fmt.Printf("Failed to delete object: %v\n", err)
				continue
			}
		}
		calculateMetrics(deleteLatencies, fmt.Sprintf("Delete (Size: %d bytes)", size), size)
	}
}

func calculateMetrics(latencies []time.Duration, operation string, dataSize int) {
	var totalDuration time.Duration
	for _, latency := range latencies {
		totalDuration += latency
	}

	// Calculate average
	avgLatency := totalDuration / time.Duration(len(latencies))

	// Calculate throughput in ops/sec
	opsPerSec := float64(len(latencies)) / totalDuration.Seconds()

	// Calculate throughput in GB/sec
	totalBytes := dataSize * len(latencies)
	gbPerSec := (float64(totalBytes) / totalDuration.Seconds()) / (1024 * 1024 * 1024)

	// Sort latencies for percentile calculations
	sortedLatencies := make([]time.Duration, len(latencies))
	copy(sortedLatencies, latencies)
	sort.Slice(sortedLatencies, func(i, j int) bool {
		return sortedLatencies[i] < sortedLatencies[j]
	})

	// Calculate min
	minLatency := sortedLatencies[0]

	// Calculate percentiles
	p90Index := int(float64(len(sortedLatencies)) * 0.9)
	p95Index := int(float64(len(sortedLatencies)) * 0.95)
	p99Index := int(float64(len(sortedLatencies)) * 0.99)

	p90Latency := sortedLatencies[p90Index]
	p95Latency := sortedLatencies[p95Index]
	p99Latency := sortedLatencies[p99Index]

	// Convert durations to milliseconds for display
	minMs := float64(minLatency.Nanoseconds()) / 1e6
	avgMs := float64(avgLatency.Nanoseconds()) / 1e6
	p90Ms := float64(p90Latency.Nanoseconds()) / 1e6
	p95Ms := float64(p95Latency.Nanoseconds()) / 1e6
	p99Ms := float64(p99Latency.Nanoseconds()) / 1e6

	fmt.Printf("\n%s Metrics:\n", operation)
	fmt.Printf("Min Latency: %.2f ms\n", minMs)
	fmt.Printf("Average Latency: %.2f ms\n", avgMs)
	fmt.Printf("P90 Latency: %.2f ms\n", p90Ms)
	fmt.Printf("P95 Latency: %.2f ms\n", p95Ms)
	fmt.Printf("P99 Latency: %.2f ms\n", p99Ms)
	fmt.Printf("Throughput: %.2f ops/sec\n", opsPerSec)
	fmt.Printf("Throughput: %.4f GB/sec\n", gbPerSec)
}
