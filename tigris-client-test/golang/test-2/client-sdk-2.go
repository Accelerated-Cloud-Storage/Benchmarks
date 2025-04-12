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
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func main() {
	fmt.Println("Tigris S3 SDK Benchmark - Test Suite 2")
	fmt.Println("===================================")

	// Run large object test
	largeObjectTest()

	// Run list operations test
	listOperationsTest()
}

// largeObjectTest tests operations with a large 10GB object
func largeObjectTest() {
	fmt.Println("\n===== LARGE OBJECT TEST =====")

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

	ctx := context.Background()

	// Create test bucket
	bucketName := fmt.Sprintf("large-object-test-%d", time.Now().UnixNano())
	fmt.Printf("\nCreating bucket: %s\n", bucketName)
	_, err = client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		fmt.Printf("Failed to create bucket: %v\n", err)
		return
	}

	defer func() {
		// Clean up bucket at the end
		fmt.Printf("\nCleaning up bucket: %s\n", bucketName)
		_, err = client.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: aws.String(bucketName),
		})
		if err != nil {
			fmt.Printf("Failed to delete bucket: %v\n", err)
		}
	}()

	// Generate 10GB of random data
	objectSize := int64(10 * 1024 * 1024 * 1024) // 10GB in bytes
	fmt.Printf("\nGenerating %.2fGB of random data...\n", float64(objectSize)/(1024*1024*1024))
	data := make([]byte, objectSize)
	_, err = rand.Read(data)
	if err != nil {
		fmt.Printf("Failed to generate random data: %v\n", err)
		return
	}

	key := "large-object"

	// Upload large object using multipart upload
	fmt.Println("\nUploading large object using multipart upload...")
	startTime := time.Now()

	// Initialize multipart upload
	createOutput, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
	})
	if err != nil {
		fmt.Printf("Failed to initialize multipart upload: %v\n", err)
		return
	}

	// Split data into chunks of 100MB
	chunkSize := int64(100 * 1024 * 1024) // 100MB chunks
	var completedParts []types.CompletedPart
	buffer := make([]byte, chunkSize)

	var partNumber int32 = 1
	for offset := int64(0); offset < objectSize; offset += chunkSize {
		// Calculate the size of the current chunk
		currentChunkSize := chunkSize
		if offset+chunkSize > objectSize {
			currentChunkSize = objectSize - offset
		}

		// Copy the chunk into the buffer
		n := copy(buffer, data[offset:offset+currentChunkSize])

		// Upload part
		partOutput, err := client.UploadPart(ctx, &s3.UploadPartInput{
			Bucket:     aws.String(bucketName),
			Key:        aws.String(key),
			PartNumber: aws.Int32(partNumber),
			UploadId:   createOutput.UploadId,
			Body:       bytes.NewReader(buffer[:n]),
		})
		if err != nil {
			// Abort multipart upload on failure
			_, abortErr := client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
				Bucket:   aws.String(bucketName),
				Key:      aws.String(key),
				UploadId: createOutput.UploadId,
			})
			if abortErr != nil {
				fmt.Printf("Failed to abort multipart upload: %v\n", abortErr)
			}
			fmt.Printf("Failed to upload part %d: %v\n", partNumber, err)
			return
		}

		completedParts = append(completedParts, types.CompletedPart{
			PartNumber: aws.Int32(partNumber),
			ETag:       partOutput.ETag,
		})

		fmt.Printf("Uploaded part %d (size: %.2f MB)\n", partNumber, float64(n)/(1024*1024))
		partNumber++
	}

	// Complete multipart upload
	_, err = client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(bucketName),
		Key:      aws.String(key),
		UploadId: createOutput.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
	})
	if err != nil {
		fmt.Printf("Failed to complete multipart upload: %v\n", err)
		return
	}

	uploadLatency := time.Since(startTime)
	calculateMetrics([]time.Duration{uploadLatency}, "Large Object Upload (Multipart)", int(objectSize))

	// Read large object
	fmt.Println("\nReading large object...")
	startTime = time.Now()
	resp, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
	})
	if err != nil {
		fmt.Printf("Failed to get object: %v\n", err)
		return
	}

	// Read all data to fully complete the operation
	retrievedData, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	downloadLatency := time.Since(startTime)
	if err != nil {
		fmt.Printf("Failed to read object data: %v\n", err)
		return
	}
	calculateMetrics([]time.Duration{downloadLatency}, "Large Object Download", int(objectSize))

	// Verify data integrity
	fmt.Println("\nVerifying data integrity...")
	if len(retrievedData) != len(data) {
		fmt.Printf("Data size mismatch! Original: %d bytes, Retrieved: %d bytes\n", len(data), len(retrievedData))
	} else if string(retrievedData) != string(data) {
		fmt.Println("Data content mismatch!")
	} else {
		fmt.Println("Data integrity verified successfully!")
	}

	// Delete large object
	fmt.Println("\nDeleting large object...")
	startTime = time.Now()
	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
	})
	deleteLatency := time.Since(startTime)
	if err != nil {
		fmt.Printf("Failed to delete object: %v\n", err)
		return
	}
	calculateMetrics([]time.Duration{deleteLatency}, "Large Object Deletion", int(objectSize))
}

// listOperationsTest tests bucket and object listing operations
func listOperationsTest() {
	fmt.Println("\n===== LIST OPERATIONS TEST =====")

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

	ctx := context.Background()

	// Part 1: Bucket List Test
	baseBucketName := fmt.Sprintf("list-test-%d", time.Now().UnixNano())
	numBuckets := 100
	var bucketNames []string

	// Create 100 buckets
	fmt.Printf("\nCreating %d buckets...\n", numBuckets)
	bucketCreateLatencies := make([]time.Duration, numBuckets)

	for i := 0; i < numBuckets; i++ {
		bucketName := fmt.Sprintf("%s-%d", baseBucketName, i)
		bucketNames = append(bucketNames, bucketName)

		startTime := time.Now()
		_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
			Bucket: aws.String(bucketName),
		})
		bucketCreateLatencies[i] = time.Since(startTime)

		if err != nil {
			fmt.Printf("Failed to create bucket %s: %v\n", bucketName, err)
			continue
		}
	}

	calculateMetrics(bucketCreateLatencies, "Bucket Creation", 0)

	// List all buckets
	fmt.Printf("\nListing all buckets...\n")
	listBucketLatencies := make([]time.Duration, 10) // Perform 10 times for reliable metrics

	for i := 0; i < 10; i++ {
		startTime := time.Now()
		_, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
		listBucketLatencies[i] = time.Since(startTime)

		if err != nil {
			fmt.Printf("Failed to list buckets: %v\n", err)
			continue
		}
	}

	calculateMetrics(listBucketLatencies, "Bucket Listing", 0)

	// Delete all buckets
	fmt.Printf("\nDeleting %d buckets...\n", numBuckets)
	bucketDeleteLatencies := make([]time.Duration, len(bucketNames))

	for i, bucketName := range bucketNames {
		startTime := time.Now()
		_, err := client.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: aws.String(bucketName),
		})
		bucketDeleteLatencies[i] = time.Since(startTime)

		if err != nil {
			fmt.Printf("Failed to delete bucket %s: %v\n", bucketName, err)
			continue
		}
	}

	calculateMetrics(bucketDeleteLatencies, "Bucket Deletion", 0)

	// Part 2: Object List Test
	objectTestBucket := fmt.Sprintf("object-list-test-%d", time.Now().UnixNano())
	numObjects := 1000

	// Create a bucket for object tests
	fmt.Printf("\nCreating bucket for object list test: %s\n", objectTestBucket)
	_, err = client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(objectTestBucket),
	})
	if err != nil {
		fmt.Printf("Failed to create bucket: %v\n", err)
		return
	}

	defer func() {
		// Clean up bucket at the end
		fmt.Printf("\nCleaning up bucket: %s\n", objectTestBucket)
		_, err = client.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: aws.String(objectTestBucket),
		})
		if err != nil {
			fmt.Printf("Failed to delete bucket: %v\n", err)
		}
	}()

	// Create 1000 small objects
	fmt.Printf("\nCreating %d objects of size 1 byte...\n", numObjects)
	objectCreateLatencies := make([]time.Duration, numObjects)
	data := []byte("0") // 1 byte of data

	for i := 0; i < numObjects; i++ {
		key := fmt.Sprintf("small-object-%d", i)

		startTime := time.Now()
		_, err := client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(objectTestBucket),
			Key:    aws.String(key),
			Body:   bytes.NewReader(data),
		})
		objectCreateLatencies[i] = time.Since(startTime)

		if err != nil {
			fmt.Printf("Failed to put object: %v\n", err)
			continue
		}
	}

	calculateMetrics(objectCreateLatencies, "Object Creation", 1)

	// List all objects
	fmt.Printf("\nListing all objects...\n")
	listObjectLatencies := make([]time.Duration, 10) // Perform 10 times

	for i := 0; i < 10; i++ {
		startTime := time.Now()
		_, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: aws.String(objectTestBucket),
		})
		listObjectLatencies[i] = time.Since(startTime)

		if err != nil {
			fmt.Printf("Failed to list objects: %v\n", err)
			continue
		}
	}

	calculateMetrics(listObjectLatencies, "Object Listing", 0)

	// Delete all objects
	fmt.Printf("\nDeleting %d objects...\n", numObjects)
	objectDeleteLatencies := make([]time.Duration, numObjects)

	for i := 0; i < numObjects; i++ {
		key := fmt.Sprintf("small-object-%d", i)

		startTime := time.Now()
		_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(objectTestBucket),
			Key:    aws.String(key),
		})
		objectDeleteLatencies[i] = time.Since(startTime)

		if err != nil {
			fmt.Printf("Failed to delete object: %v\n", err)
			continue
		}
	}

	calculateMetrics(objectDeleteLatencies, "Object Deletion", 1)
}

func calculateMetrics(latencies []time.Duration, operation string, dataSize int) {
	if len(latencies) == 0 {
		fmt.Printf("No valid latencies for %s\n", operation)
		return
	}

	// Sort latencies for percentile calculations
	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})

	// Calculate min and average
	minLatency := latencies[0]
	var totalDuration time.Duration
	for _, latency := range latencies {
		totalDuration += latency
	}
	avgLatency := totalDuration / time.Duration(len(latencies))

	// Calculate throughput in ops/sec
	opsPerSec := float64(len(latencies)) / totalDuration.Seconds()

	// Calculate throughput in GB/sec if data_size is provided
	var gbPerSec float64
	if dataSize > 0 {
		totalBytes := dataSize * len(latencies)
		gbPerSec = (float64(totalBytes) / totalDuration.Seconds()) / (1024 * 1024 * 1024)
	}

	// Calculate percentiles
	p90Index := int(float64(len(latencies)) * 0.9)
	p95Index := int(float64(len(latencies)) * 0.95)
	p99Index := int(float64(len(latencies)) * 0.99)

	p90Latency := latencies[p90Index]
	p95Latency := latencies[p95Index]
	p99Latency := latencies[p99Index]

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
	if dataSize > 0 {
		fmt.Printf("Throughput: %.6f GB/sec\n", gbPerSec)
	}
}
