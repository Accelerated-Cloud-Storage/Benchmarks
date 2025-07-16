package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"math/rand"
	"context"
	"sort"
	"time"

	valkey "github.com/valkey-io/valkey-go"
)

func main() {
	const host = "test-valkey-standard-1-geapva.serverless.use1.cache.amazonaws.com" 
	ctx := context.Background()
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{host + ":6379"},
		TLSConfig:   &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12},
		DisableCache: true,
	})
	if err != nil {
		log.Fatalf("create Valkey client: %v", err)
	}
	defer client.Close()

	// Create a unique key prefix for testing
	keyPrefix := fmt.Sprintf("test_%d", time.Now().UnixNano())

	// Cleanup function
	defer func() {
		fmt.Printf("Cleaning up test keys with prefix: %s\n", keyPrefix)
		// Use SCAN to find and delete all test keys
		cursor := uint64(0)
		for {
			scanResult, err := client.Do(ctx, client.B().Scan().Cursor(cursor).Match(keyPrefix+"*").Count(100).Build()).AsScanEntry()
			if err != nil {
				fmt.Printf("Failed to scan keys: %v\n", err)
				break
			}

			if len(scanResult.Elements) > 0 {
				// Delete keys in batches
				for _, key := range scanResult.Elements {
					client.Do(ctx, client.B().Del().Key(key).Build())
				}
			}

			cursor = scanResult.Cursor
			if cursor == 0 {
				break
			}
		}
	}()

	// Define test data sizes
	dataSizes := []int{1024, 1024 * 1024, 10 * 1024 * 1024} // 1KB, 1MB, 10MB
	numOperations := 100

	// Step 1: String SET operations
	fmt.Println("Starting string SET operations for varying data sizes...")
	for _, size := range dataSizes {
		fmt.Printf("\nPerforming %d SET operations with %d bytes data\n", numOperations, size)
		setLatencies := make([]time.Duration, numOperations)

		for i := 0; i < numOperations; i++ {
			key := fmt.Sprintf("%s_string_%d_size_%d", keyPrefix, i, size)
			data := make([]byte, size)
			rand.Read(data)
			value := string(data)

			start := time.Now()
			err := client.Do(ctx, client.B().Set().Key(key).Value(value).Build()).Error()
			setLatencies[i] = time.Since(start)

			if err != nil {
				fmt.Printf("Failed to set key %s: %v\n", key, err)
				continue
			}
		}
		calculateMetrics(setLatencies, fmt.Sprintf("String SET (Size: %d bytes)", size), size)
	}

	// Step 2: String GET operations
	fmt.Println("\nStarting string GET operations for varying data sizes...")
	for _, size := range dataSizes {
		fmt.Printf("\nPerforming %d GET operations with %d bytes data\n", numOperations, size)
		getLatencies := make([]time.Duration, numOperations)

		for i := 0; i < numOperations; i++ {
			key := fmt.Sprintf("%s_string_%d_size_%d", keyPrefix, i, size)

			start := time.Now()
			_, err := client.Do(ctx, client.B().Get().Key(key).Build()).ToString()
			getLatencies[i] = time.Since(start)

			if err != nil {
				fmt.Printf("Failed to get key %s: %v\n", key, err)
				continue
			}
		}
		calculateMetrics(getLatencies, fmt.Sprintf("String GET (Size: %d bytes)", size), size)
	}

	// Step 3: Delete operations
	fmt.Println("\nStarting delete operations...")
	deleteLatencies := make([]time.Duration, numOperations)

	for i := 0; i < numOperations; i++ {
		key := fmt.Sprintf("%s_string_%d_size_%d", keyPrefix, i, dataSizes[0])

		start := time.Now()
		err := client.Do(ctx, client.B().Del().Key(key).Build()).Error()
		deleteLatencies[i] = time.Since(start)

		if err != nil {
			fmt.Printf("Failed to delete key %s: %v\n", key, err)
			continue
		}
	}
	calculateMetrics(deleteLatencies, "Key DELETE", dataSizes[0])

	fmt.Println("\nValkey benchmark completed successfully!")
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

	// Calculate throughput in MB/sec (approximate)
	totalBytes := dataSize * len(latencies)
	mbPerSec := (float64(totalBytes) / totalDuration.Seconds()) / (1024 * 1024)

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

	if p90Index >= len(sortedLatencies) {
		p90Index = len(sortedLatencies) - 1
	}
	if p95Index >= len(sortedLatencies) {
		p95Index = len(sortedLatencies) - 1
	}
	if p99Index >= len(sortedLatencies) {
		p99Index = len(sortedLatencies) - 1
	}

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
	fmt.Printf("Throughput: %.4f MB/sec\n", mbPerSec)
}