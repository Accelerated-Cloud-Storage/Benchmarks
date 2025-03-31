package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"sort"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// percentile calculates the p-th percentile of a sorted slice of float64 values.
func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0.0
	}
	k := float64(len(values)-1) * p
	f := int(k)
	c := f + 1
	if c >= len(values) {
		c = len(values) - 1
	}
	d0 := values[f] * (float64(c) - k)
	d1 := values[c] * (k - float64(f))
	return d0 + d1
}

// calculateMetrics calculates and prints performance metrics for a set of latencies.
func calculateMetrics(latencies []time.Duration, operation string, dataSize int64) {
	if len(latencies) == 0 {
		fmt.Printf("\nNo latencies recorded for %s\n", operation)
		return
	}

	// Convert durations to float64 seconds for calculations
	floatLatencies := make([]float64, len(latencies))
	var totalLatency time.Duration
	for i, lat := range latencies {
		floatLatencies[i] = lat.Seconds()
		totalLatency += lat
	}

	// Sort latencies for percentile calculations
	sort.Float64s(floatLatencies)

	// Calculate basic statistics
	minLatency := floatLatencies[0]
	avgLatency := totalLatency.Seconds() / float64(len(latencies))

	// Calculate throughput in ops/sec
	opsPerSec := float64(len(latencies)) / totalLatency.Seconds()

	// Calculate throughput in GB/sec if dataSize is provided
	var gbPerSec float64
	if dataSize > 0 {
		totalBytes := dataSize * int64(len(latencies))
		gbPerSec = (float64(totalBytes) / totalLatency.Seconds()) / (1024 * 1024 * 1024)
	}

	// Calculate percentiles
	p90 := percentile(floatLatencies, 0.90)
	p95 := percentile(floatLatencies, 0.95)
	p99 := percentile(floatLatencies, 0.99)

	// Convert latencies from seconds to milliseconds for printing
	minMs := minLatency * 1000
	avgMs := avgLatency * 1000
	p90Ms := p90 * 1000
	p95Ms := p95 * 1000
	p99Ms := p99 * 1000

	fmt.Printf("\n%s Metrics:\n", operation)
	fmt.Printf("Min Latency: %.2f ms\n", minMs)
	fmt.Printf("Average Latency: %.2f ms\n", avgMs)
	fmt.Printf("P90 Latency: %.2f ms\n", p90Ms)
	fmt.Printf("P95 Latency: %.2f ms\n", p95Ms)
	fmt.Printf("P99 Latency: %.2f ms\n", p99Ms)
	fmt.Printf("Throughput: %.2f ops/sec\n", opsPerSec)
	if dataSize > 0 {
		fmt.Printf("Throughput: %.4f GB/sec\n", gbPerSec)
	}
}

func main() {
	region := "us-east-1"
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
	if err != nil {
		log.Fatalf("Unable to load SDK config, %v", err)
	}

	s3Client := s3.NewFromConfig(cfg)
	ctx := context.TODO()

	// Create a unique bucket name for Express One Zone
	baseName := "go-express-bucket-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	zoneID := "use1-az6" // Zone ID for us-east-1c
	bucketName := fmt.Sprintf("%s--%s--x-s3", baseName, zoneID)

	var bucketCreated bool

	// Ensure bucket cleanup happens even on errors after creation
	defer func() {
		if bucketCreated {
			cleanupBucket(ctx, s3Client, bucketName)
		}
	}()

	// Create Express One Zone directory bucket
	fmt.Printf("Creating Express One Zone directory bucket: %s\n", bucketName)
	_, err = s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
		CreateBucketConfiguration: &types.CreateBucketConfiguration{
			Bucket: &types.BucketInfo{
				Type:           types.BucketTypeDirectory,
				DataRedundancy: types.DataRedundancySingleAvailabilityZone,
			},
			Location: &types.LocationInfo{
				Name: aws.String(zoneID),
				Type: types.LocationTypeAvailabilityZone,
			},
		},
	})
	if err != nil {
		log.Fatalf("Failed to create bucket %s: %v", bucketName, err)
	}
	bucketCreated = true
	fmt.Printf("Successfully created bucket %s\n", bucketName)

	// Allow some time for bucket creation to propagate (optional but can help prevent immediate errors)
	// time.Sleep(5 * time.Second)

	// Define test object sizes
	objectSizes := []int64{1024, 1024 * 1024, 10 * 1024 * 1024} // 1KB, 1MB, 10MB
	numObjects := 50

	// --- Step 1: Write objects ---
	fmt.Println("\nStarting write operations for varying object sizes...")
	for _, size := range objectSizes {
		fmt.Printf("\nWriting %d objects of size %d bytes\n", numObjects, size)
		writeLatencies := make([]time.Duration, 0, numObjects)
		data := make([]byte, size)

		for i := 0; i < numObjects; i++ {
			key := fmt.Sprintf("key_%d_size_%d", i, size)
			_, err := rand.Read(data) // Generate random data for each object
			if err != nil {
				log.Printf("Failed to generate random data for %s: %v", key, err)
				continue
			}

			start := time.Now()
			_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
				Bucket: aws.String(bucketName),
				Key:    aws.String(key),
				Body:   bytes.NewReader(data), // Corrected Body type
			})
			latency := time.Since(start)

			if err != nil {
				log.Printf("Failed to put object %s: %v", key, err)
			} else {
				writeLatencies = append(writeLatencies, latency)
			}
		}
		calculateMetrics(writeLatencies, fmt.Sprintf("Write (Size: %d bytes)", size), size)
	}

	// --- Step 2: Read objects ---
	fmt.Println("\nStarting read operations for varying object sizes...")
	for _, size := range objectSizes {
		fmt.Printf("\nReading %d objects of size %d bytes\n", numObjects, size)
		readLatencies := make([]time.Duration, 0, numObjects)

		for i := 0; i < numObjects; i++ {
			key := fmt.Sprintf("key_%d_size_%d", i, size)

			start := time.Now() // Start timer before GetObject
			resp, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
				Bucket: aws.String(bucketName),
				Key:    aws.String(key),
			})

			if err != nil {
				log.Printf("Failed to get object %s: %v", key, err)
			} else {
				// Read and close the body to complete the operation
				_, readErr := io.ReadAll(resp.Body)
				closeErr := resp.Body.Close()
				latency := time.Since(start) // Measure latency AFTER reading and closing body

				if readErr != nil {
					log.Printf("Failed to read body for object %s: %v", key, readErr)
					// Optionally skip appending latency if read fails
					continue
				}
				if closeErr != nil {
					log.Printf("Failed to close body for object %s: %v", key, closeErr)
					// Optionally skip appending latency if close fails
					continue
				}
				readLatencies = append(readLatencies, latency) // Append latency including read time
			}
		}
		calculateMetrics(readLatencies, fmt.Sprintf("Read (Size: %d bytes)", size), size)
	}

	// --- Step 3: Delete objects ---
	// Note: Cleanup function called via defer handles deletion,
	// but we can measure individual deletes here if needed.
	fmt.Println("\nStarting delete operations...")
	for _, size := range objectSizes {
		fmt.Printf("\nDeleting %d objects of size %d bytes\n", numObjects, size)
		deleteLatencies := make([]time.Duration, 0, numObjects)

		for i := 0; i < numObjects; i++ {
			key := fmt.Sprintf("key_%d_size_%d", i, size)

			start := time.Now()
			_, err := s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(bucketName),
				Key:    aws.String(key),
			})
			latency := time.Since(start)

			if err != nil {
				log.Printf("Failed to delete object %s: %v", key, err)
			} else {
				deleteLatencies = append(deleteLatencies, latency)
			}
		}
		calculateMetrics(deleteLatencies, fmt.Sprintf("Delete (Size: %d bytes)", size), 0) // dataSize = 0 for delete
	}
}

// cleanupBucket deletes all objects in the bucket and then deletes the bucket itself.
func cleanupBucket(ctx context.Context, s3Client *s3.Client, bucketName string) {
	fmt.Printf("\nCleaning up bucket: %s\n", bucketName)

	// List and delete all objects
	var objectsToDelete []types.ObjectIdentifier
	paginator := s3.NewListObjectsV2Paginator(s3Client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			log.Printf("Failed to list objects for cleanup in %s: %v", bucketName, err)
			// Proceed to try deleting the bucket anyway, might fail if not empty
			break
		}
		for _, obj := range page.Contents {
			objectsToDelete = append(objectsToDelete, types.ObjectIdentifier{Key: obj.Key})
		}
	}

	// Batch delete objects (up to 1000 per request)
	if len(objectsToDelete) > 0 {
		// S3 batch delete takes max 1000 keys
		chunkSize := 1000
		for i := 0; i < len(objectsToDelete); i += chunkSize {
			end := i + chunkSize
			if end > len(objectsToDelete) {
				end = len(objectsToDelete)
			}
			chunk := objectsToDelete[i:end]

			fmt.Printf("Deleting %d objects...\n", len(chunk))
			_, err := s3Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
				Bucket: aws.String(bucketName),
				Delete: &types.Delete{Objects: chunk},
			})
			if err != nil {
				log.Printf("Failed to delete object chunk in %s: %v", bucketName, err)
				// Continue trying to delete other chunks/bucket
			}
		}
	} else {
		fmt.Println("No objects found to delete.")
	}

	// Delete bucket
	fmt.Println("Deleting bucket...")
	_, err := s3Client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		log.Printf("Failed to delete bucket %s: %v", bucketName, err)
	} else {
		fmt.Println("Successfully deleted bucket.")
	}
}
