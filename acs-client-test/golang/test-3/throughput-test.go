// Copyright 2025 Accelerated Cloud Storage Corporation. All Rights Reserved.
package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"sync"
	"time"

	client "github.com/AcceleratedCloudStorage/acs-sdk-go/client"
)

func main() {
	// Create results directory if it doesn't exist
	if err := os.MkdirAll("results", 0755); err != nil {
		fmt.Printf("Failed to create results directory: %v\n", err)
		os.Exit(1)
	}

	// Set up profiling
	if err := setupProfiling(); err != nil {
		fmt.Printf("Failed to setup profiling: %v\n", err)
		os.Exit(1)
	}
	defer cleanupProfiling()

	// Initialize client with retries
	acsClient, err := initializeClient()
	if err != nil {
		fmt.Printf("Failed to create client: %v\n", err)
		os.Exit(1)
	}
	defer acsClient.Close()

	// Create a unique bucket for testing
	bucket := fmt.Sprintf("throughput-test-%d", time.Now().UnixNano())
	ctx := context.Background()

	// Create bucket with retry
	if err := createBucketWithRetry(ctx, acsClient, bucket); err != nil {
		fmt.Printf("Failed to create bucket: %v\n", err)
		os.Exit(1)
	}
	defer cleanupBucket(ctx, acsClient, bucket)

	// Initialize metrics collector
	collector := NewMetricsCollector()
	collector.Start()
	defer collector.Stop()

	// Write test configuration
	writeTestConfig()

	fmt.Println("\n--- THROUGHPUT BENCHMARK RESULTS ---")
	printHeader()

	// Test different object sizes with warm-up
	objectSizes := getTestCases()

	// Warm-up phase
	fmt.Println("\nPerforming warm-up...")
	warmupSize := int64(1 * 1024 * 1024) // 1MB
	err = runConcurrentOperations(ctx, acsClient, bucket, warmupSize, 10, 5, PUT, collector)
	if err != nil {
		fmt.Printf("Warm-up failed: %v\n", err)
	}
	time.Sleep(2 * time.Second)

	// Main test loop
	for _, test := range objectSizes {
		fmt.Printf("\nTesting with %d MB files (%d objects, %d concurrent):\n",
			test.size/(1024*1024), test.numObjects, test.concurrent)

		// Run PUT tests
		err = runConcurrentOperations(ctx, acsClient, bucket, test.size, test.numObjects, test.concurrent, PUT, collector)
		if err != nil {
			fmt.Printf("PUT phase failed: %v\n", err)
			continue
		}
		metrics := collector.GetMetrics()
		printDetailedResults("PUT", test.size, &metrics)

		// Cooldown period
		time.Sleep(5 * time.Second)

		// Run GET tests
		err = runConcurrentOperations(ctx, acsClient, bucket, test.size, test.numObjects, test.concurrent, GET, collector)
		if err != nil {
			fmt.Printf("GET phase failed: %v\n", err)
			continue
		}
		metrics = collector.GetMetrics()
		printDetailedResults("GET", test.size, &metrics)

		// Clean up test objects
		if err := cleanupObjects(ctx, acsClient, bucket, test.numObjects); err != nil {
			fmt.Printf("Warning: Failed to cleanup some objects: %v\n", err)
		}

		// Write results to file after each test case
		if err := collector.WriteResultsToFile(fmt.Sprintf("results/metrics_%dMB.json", test.size/(1024*1024))); err != nil {
			fmt.Printf("Warning: Failed to write metrics: %v\n", err)
		}
	}
}

type testCase struct {
	size       int64
	numObjects int
	concurrent int
}

func getTestCases() []testCase {
	return []testCase{
		{1 * 1024 * 1024, 1000, 50}, // 1MB files
		{10 * 1024 * 1024, 100, 20}, // 10MB files
		{100 * 1024 * 1024, 20, 10}, // 100MB files
		{500 * 1024 * 1024, 5, 5},   // 500MB files
		{1024 * 1024 * 1024, 2, 2},  // 1GB files
	}
}

func setupProfiling() error {
	// CPU profile
	cpuFile, err := os.Create("results/cpu.prof")
	if err != nil {
		return fmt.Errorf("create CPU profile: %v", err)
	}
	if err := pprof.StartCPUProfile(cpuFile); err != nil {
		cpuFile.Close()
		return fmt.Errorf("start CPU profile: %v", err)
	}

	// Trace profile
	traceFile, err := os.Create("results/trace.out")
	if err != nil {
		return fmt.Errorf("create trace file: %v", err)
	}
	if err := trace.Start(traceFile); err != nil {
		traceFile.Close()
		return fmt.Errorf("start trace: %v", err)
	}

	// Other profiles
	runtime.SetBlockProfileRate(1)
	runtime.SetMutexProfileFraction(1)

	return nil
}

func cleanupProfiling() {
	pprof.StopCPUProfile()
	trace.Stop()

	// Write other profiles
	writeProfile("heap", "results/mem.prof")
	writeProfile("goroutine", "results/goroutine.prof")
	writeProfile("block", "results/block.prof")
	writeProfile("mutex", "results/mutex.prof")
}

func writeProfile(name, path string) {
	f, err := os.Create(path)
	if err != nil {
		fmt.Printf("Failed to create %s profile: %v\n", name, err)
		return
	}
	defer f.Close()
	if err := pprof.Lookup(name).WriteTo(f, 0); err != nil {
		fmt.Printf("Failed to write %s profile: %v\n", name, err)
	}
}

func initializeClient() (*client.ACSClient, error) {
	maxRetries := 3
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		client, err := client.NewClient(&client.Session{
			Region: "us-east-1",
		})
		if err == nil {
			return client, nil
		}
		lastErr = err
		time.Sleep(time.Second * time.Duration(i+1))
	}
	return nil, fmt.Errorf("failed after %d retries: %v", maxRetries, lastErr)
}

func createBucketWithRetry(ctx context.Context, client *client.ACSClient, bucket string) error {
	maxRetries := 3
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if err := client.CreateBucket(ctx, bucket); err == nil {
			return nil
		} else {
			lastErr = err
			time.Sleep(time.Second * time.Duration(i+1))
		}
	}
	return fmt.Errorf("failed after %d retries: %v", maxRetries, lastErr)
}

func cleanupBucket(ctx context.Context, client *client.ACSClient, bucket string) {
	fmt.Printf("\nCleaning up bucket: %s\n", bucket)
	if err := client.DeleteBucket(ctx, bucket); err != nil {
		fmt.Printf("Warning: Failed to delete bucket: %v\n", err)
	}
}

func printHeader() {
	fmt.Println("Operation  Size(MB)  Objects  Concurrent  Throughput(MB/s)  Avg Latency  Success/Total  CPU%  Mem%")
	fmt.Println("------------------------------------------------------------------------------------------------")
}

func printDetailedResults(opType string, size int64, metrics *SystemMetrics) {
	var opMetrics OperationMetrics
	if opType == "PUT" {
		opMetrics = metrics.PutMetrics
	} else {
		opMetrics = metrics.GetMetrics
	}

	fmt.Printf("\n%s Results for %dMB files:\n", opType, size/(1024*1024))
	fmt.Printf("Throughput: %.2f MB/s\n", opMetrics.Throughput)
	fmt.Printf("Operations: %d successful out of %d total (%.1f%%)\n",
		opMetrics.SuccessCount, opMetrics.TotalOperations,
		float64(opMetrics.SuccessCount)/float64(opMetrics.TotalOperations)*100)
	fmt.Printf("Latency: avg=%v, min=%v, max=%v\n",
		opMetrics.AvgLatency, opMetrics.MinLatency, opMetrics.MaxLatency)
	fmt.Printf("Timing: compression=%v, transfer=%v\n",
		opMetrics.AvgCompressTime, opMetrics.AvgTransferTime)

	fmt.Printf("\nNetwork Metrics:\n")
	fmt.Printf("  Bytes Sent: %d MB\n", metrics.BytesSent/(1024*1024))
	fmt.Printf("  Bytes Received: %d MB\n", metrics.BytesReceived/(1024*1024))
	fmt.Printf("  Network Throughput: %.2f MB/s\n", metrics.NetworkThroughput)
	fmt.Printf("  TCP Retransmits: %d\n", metrics.TCPRetransmits)
	fmt.Printf("  Active TCP Connections: %d\n", metrics.TCPConnections)
	fmt.Printf("  Network Latency: %v\n", metrics.NetworkLatency)

	fmt.Printf("\nSystem Metrics:\n")
	fmt.Printf("  CPU Usage: %.1f%%\n", metrics.CPUUsage)
	fmt.Printf("  Memory Usage: %.1f%%\n", metrics.MemoryUsage)
	fmt.Printf("  Memory Allocated: %d MB\n", metrics.MemoryAllocated/(1024*1024))
}

func writeTestConfig() {
	config := fmt.Sprintf("Test Configuration:\n"+
		"Go Version: %s\n"+
		"GOMAXPROCS: %d\n"+
		"OS/Arch: %s/%s\n"+
		"Time: %s\n",
		runtime.Version(),
		runtime.GOMAXPROCS(0),
		runtime.GOOS,
		runtime.GOARCH,
		time.Now().Format(time.RFC3339))

	if err := os.WriteFile("results/config.txt", []byte(config), 0644); err != nil {
		fmt.Printf("Warning: Failed to write test configuration: %v\n", err)
	}
}

// runConcurrentOperations executes operations concurrently and collects metrics
func runConcurrentOperations(ctx context.Context, client *client.ACSClient, bucket string, objectSize int64, numObjects int, concurrency int, opType OperationType, collector *MetricsCollector) error {
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	errChan := make(chan error, numObjects)

	data := make([]byte, objectSize)
	rand.Read(data)

	for i := 0; i < numObjects; i++ {
		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore

		go func(objNum int) {
			defer func() {
				<-sem // Release semaphore
				wg.Done()
			}()

			objectName := fmt.Sprintf("test-object-%d", objNum)
			start := time.Now()
			var compressStart, compressEnd, transferStart, transferEnd time.Time
			var err error

			switch opType {
			case PUT:
				compressStart = time.Now()
				// Compression happens internally in the SDK
				compressEnd = time.Now()
				transferStart = time.Now()
				err = client.PutObject(ctx, bucket, objectName, data)
				transferEnd = time.Now()
			case GET:
				transferStart = time.Now()
				_, err = client.GetObject(ctx, bucket, objectName)
				transferEnd = time.Now()
				// No compression timing for GET
				compressStart = transferEnd
				compressEnd = transferEnd
			}

			if err != nil {
				errChan <- fmt.Errorf("operation %s failed for object %s: %v", opType, objectName, err)
			}

			collector.RecordOperationMetrics(opType, start, compressStart, compressEnd, transferStart, transferEnd, uint64(objectSize), err)
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for any errors
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to process %d objects", len(errors))
	}
	return nil
}

func cleanupObjects(ctx context.Context, client *client.ACSClient, bucket string, numObjects int) error {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 20) // Limit concurrent deletions
	errChan := make(chan error, numObjects)

	for i := 0; i < numObjects; i++ {
		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore

		go func(objNum int) {
			defer func() {
				<-sem // Release semaphore
				wg.Done()
			}()

			objectName := fmt.Sprintf("test-object-%d", objNum)
			if err := client.DeleteObject(ctx, bucket, objectName); err != nil {
				errChan <- fmt.Errorf("failed to delete object %s: %v", objectName, err)
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for any errors
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to delete %d objects", len(errors))
	}
	return nil
}
