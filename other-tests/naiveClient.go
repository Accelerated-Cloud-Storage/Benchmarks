package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

const (
	bucketName = "nkdev-naive-bucket"
	region     = "us-east-1"
	numObjects = 50
)

var s3Client *s3.Client
var objectSizes = []int{1024, 1024 * 1024, 1024 * 1024 * 10} // 1 KB, 1 MB, 10 MB

func init() {
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
	if err != nil {
		log.Fatalf("Unable to load SDK config, %v", err)
	}
	s3Client = s3.NewFromConfig(cfg)
}

func main() {
	ctx := context.Background()

	// Run tests
	Test1(ctx)
	Test2(ctx)
	Test3(ctx)
	Test4(ctx)
	Test5(ctx)
	Test6(ctx)
	Test7(ctx)
	Test8(ctx)
}

func Test1(ctx context.Context) {
	// Test 1: Create bucket, run object storage APIs, and delete bucket
	/*
	   fmt.Println("\n=== Test 1: Basic Object Storage Operations ===")
	   fmt.Printf("Creating bucket: %s\n", bucketName)
	   if err := CreateBucketS3(ctx, bucketName); err != nil {
	       log.Printf("Warning: Failed to create bucket: %v", err)
	   }
	*/

	// Run object storage APIs
	fmt.Println("\n=== Test: Basic Object Storage Operations ===")
	ObjectAPIsTest("nkdev-1737867673485441710")

	// Delete bucket
	/*
	   fmt.Printf("Deleting bucket: %s\n", bucketName)
	   if err := DeleteBucketS3(ctx, bucketName); err != nil {
	       log.Printf("Warning: Failed to delete bucket: %v", err)
	   }
	*/
}

func Test2(ctx context.Context) {
	// Test 2: Multi-bucket operations
	fmt.Println("\n=== Test 2: Multi-bucket Operations ===")
	sourceBucket := bucketName + "-source"
	destBucket := bucketName + "-dest"

	// Create two buckets
	fmt.Printf("Creating source bucket: %s\n", sourceBucket)
	if err := CreateBucketS3(ctx, sourceBucket); err != nil {
		log.Printf("Warning: Failed to create source bucket: %v", err)
		return
	}

	fmt.Printf("Creating destination bucket: %s\n", destBucket)
	if err := CreateBucketS3(ctx, destBucket); err != nil {
		log.Printf("Warning: Failed to create destination bucket: %v", err)
		return
	}

	// List buckets
	fmt.Println("\nListing buckets:")
	if buckets, err := ListBucketsS3(ctx); err == nil {
		for _, bucket := range buckets {
			fmt.Printf("- %s\n", *bucket.Name)
		}
	}

	// Create 10 objects in source bucket and get their metadata
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("test-object-%d", i)
		data := []byte(fmt.Sprintf("test data %d", i))
		if err := PutObjectS3(key, data, sourceBucket); err != nil {
			log.Printf("Failed to create object %s: %v", key, err)
			continue
		}

		// Get and display object metadata
		metadata, err := GetObjectMetadataS3(ctx, sourceBucket, key)
		if err != nil {
			log.Printf("Failed to get metadata for object %s: %v", key, err)
		} else {
			fmt.Printf("\nMetadata for object %s:\n", key)
			fmt.Printf("- Content Length: %d bytes\n", metadata.ContentLength)
			fmt.Printf("- ETag: %s\n", *metadata.ETag)
			fmt.Printf("- Last Modified: %v\n", metadata.LastModified)
			fmt.Printf("- Version ID: %s\n", metadata.VersionId)
			fmt.Printf("- Server Side Encryption: %s\n", metadata.ServerSideEncryption)
			fmt.Printf("- Metadata: %v\n", metadata.Metadata)
		}
	}

	// List objects in source bucket
	fmt.Println("\nListing objects in source bucket:")
	if objects, err := ListObjectsS3(ctx, sourceBucket); err == nil {
		for _, obj := range objects {
			fmt.Printf("- %s\n", *obj.Key)
		}
	}

	// Copy first object to destination bucket
	testKey := "test-object-0"
	fmt.Printf("\nCopying object %s from %s to %s\n", testKey, sourceBucket, destBucket)
	if err := CopyToBucketS3(ctx, sourceBucket, destBucket, testKey); err != nil {
		log.Printf("Failed to copy object: %v", err)
	}

	// Clean up
	fmt.Println("\nCleaning up resources...")
	if err := DeleteBucketS3(ctx, sourceBucket); err != nil {
		log.Printf("Warning: Failed to delete source bucket: %v", err)
	}
	if err := DeleteBucketS3(ctx, destBucket); err != nil {
		log.Printf("Warning: Failed to delete destination bucket: %v", err)
	}
}

func Test3(ctx context.Context) {
	fmt.Println("\n=== Test 3: Multi-part Upload and Range-based Downloads ===")
	testBucket := bucketName + "-multipart"

	// Create test bucket
	fmt.Printf("Creating bucket: %s\n", testBucket)
	if err := CreateBucketS3(ctx, testBucket); err != nil {
		log.Printf("Failed to create bucket: %v", err)
		return
	}

	// Create a large file (20MB) for multi-part upload
	fileSize := 20 * 1024 * 1024 // 20MB
	data := make([]byte, fileSize)
	rand.Read(data)
	key := "large-test-file"

	// Start multi-part upload
	createInput := &s3.CreateMultipartUploadInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
	}
	createOutput, err := s3Client.CreateMultipartUpload(ctx, createInput)
	if err != nil {
		log.Printf("Failed to create multipart upload: %v", err)
		return
	}

	// Upload parts (5MB chunks)
	var completedParts []types.CompletedPart
	partSize := 5 * 1024 * 1024 // 5MB per part
	partNum := 1

	for start := 0; start < len(data); start += partSize {
		end := min(start+partSize, len(data))
		fmt.Printf("Uploading part %d...\n", partNum)

		partNumber := int32(partNum)
		contentLength := int64(end - start)

		uploadInput := &s3.UploadPartInput{
			Bucket:        aws.String(testBucket),
			Key:           aws.String(key),
			PartNumber:    &partNumber,
			UploadId:      createOutput.UploadId,
			Body:          bytes.NewReader(data[start:end]),
			ContentLength: &contentLength,
		}

		uploadOutput, err := s3Client.UploadPart(ctx, uploadInput)
		if err != nil {
			log.Printf("Failed to upload part %d: %v", partNum, err)
			// Abort upload on failure
			_, _ = s3Client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
				Bucket:   aws.String(testBucket),
				Key:      aws.String(key),
				UploadId: createOutput.UploadId,
			})
			return
		}

		completedParts = append(completedParts, types.CompletedPart{
			ETag:       uploadOutput.ETag,
			PartNumber: &partNumber,
		})
		partNum++
	}

	// Complete multi-part upload
	completeInput := &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(testBucket),
		Key:      aws.String(key),
		UploadId: createOutput.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
	}

	_, err = s3Client.CompleteMultipartUpload(ctx, completeInput)
	if err != nil {
		log.Printf("Failed to complete multipart upload: %v", err)
		return
	}
	fmt.Println("Multi-part upload completed successfully")

	// Test range-based downloads
	ranges := [][2]int64{
		{0, 999},
		{5000, 5999},
		{int64(fileSize - 1000), int64(fileSize - 1)},
	}
	for _, r := range ranges {
		fmt.Printf("\nDownloading byte range %d-%d...\n", r[0], r[1])
		input := &s3.GetObjectInput{
			Bucket: aws.String(testBucket),
			Key:    aws.String(key),
			Range:  aws.String(fmt.Sprintf("bytes=%d-%d", r[0], r[1])),
		}

		result, err := s3Client.GetObject(ctx, input)
		if err != nil {
			log.Printf("Failed to download range %d-%d: %v", r[0], r[1], err)
			continue
		}

		rangeData, err := io.ReadAll(result.Body)
		result.Body.Close()
		if err != nil {
			log.Printf("Failed to read range data: %v", err)
			continue
		}

		fmt.Printf("Successfully downloaded %d bytes\n", len(rangeData))
		// Verify the data matches the original
		if !bytes.Equal(rangeData, data[r[0]:r[1]+1]) {
			fmt.Println("Warning: Downloaded data doesn't match original!")
		}
	}

	// Clean up
	fmt.Println("\nCleaning up resources...")
	if err := DeleteBucketS3(ctx, testBucket); err != nil {
		log.Printf("Warning: Failed to delete bucket: %v", err)
	}
}

func Test4(ctx context.Context) {
	fmt.Println("\n=== Test 4: Object Tagging Operations ===")
	testBucket := bucketName + "-tags"
	testKey := "tagged-object"
	testData := []byte("This is a test object with tags")

	// Create test bucket
	fmt.Printf("Creating bucket: %s\n", testBucket)
	if err := CreateBucketS3(ctx, testBucket); err != nil {
		log.Printf("Failed to create bucket: %v", err)
		return
	}

	// Put object
	fmt.Printf("Putting object: %s\n", testKey)
	if err := PutObjectS3(testKey, testData, testBucket); err != nil {
		log.Printf("Failed to put object: %v", err)
		return
	}

	// Put tags
	tags := types.Tagging{
		TagSet: []types.Tag{
			{Key: aws.String("environment"), Value: aws.String("test")},
			{Key: aws.String("purpose"), Value: aws.String("demo")},
			{Key: aws.String("created"), Value: aws.String(time.Now().Format(time.RFC3339))},
		},
	}

	putTagsInput := &s3.PutObjectTaggingInput{
		Bucket:  aws.String(testBucket),
		Key:     aws.String(testKey),
		Tagging: &tags,
	}

	fmt.Println("Adding tags to object...")
	if _, err := s3Client.PutObjectTagging(ctx, putTagsInput); err != nil {
		log.Printf("Failed to put object tags: %v", err)
		return
	}

	// Get and display object tags
	getTagsInput := &s3.GetObjectTaggingInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(testKey),
	}

	fmt.Println("\nRetrieving object tags...")
	if tagsOutput, err := s3Client.GetObjectTagging(ctx, getTagsInput); err != nil {
		log.Printf("Failed to get object tags: %v", err)
	} else {
		fmt.Println("Object tags:")
		for _, tag := range tagsOutput.TagSet {
			fmt.Printf("- %s: %s\n", *tag.Key, *tag.Value)
		}
	}

	// Get object and verify data
	fmt.Println("\nRetrieving object data...")
	if data, err := GetObjectS3(testKey, testBucket); err != nil {
		log.Printf("Failed to get object: %v", err)
	} else {
		fmt.Printf("Object content: %s\n", string(data))
	}

	// Delete tags
	deleteTagsInput := &s3.DeleteObjectTaggingInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(testKey),
	}

	fmt.Println("\nDeleting object tags...")
	if _, err := s3Client.DeleteObjectTagging(ctx, deleteTagsInput); err != nil {
		log.Printf("Failed to delete object tags: %v", err)
	}

	// Verify tags are deleted
	fmt.Println("Verifying tags are deleted...")
	if tagsOutput, err := s3Client.GetObjectTagging(ctx, getTagsInput); err != nil {
		log.Printf("Failed to get object tags: %v", err)
	} else {
		if len(tagsOutput.TagSet) == 0 {
			fmt.Println("All tags successfully deleted")
		} else {
			fmt.Printf("Warning: %d tags still remain\n", len(tagsOutput.TagSet))
		}
	}

	// Clean up
	fmt.Println("\nCleaning up resources...")
	if err := DeleteBucketS3(ctx, testBucket); err != nil {
		log.Printf("Warning: Failed to delete bucket: %v", err)
	}
}

func Test5(ctx context.Context) {
	fmt.Println("\n=== Test 5: Advanced ListObjects Operations ===")
	testBucket := bucketName + "-listing"

	// Create test bucket
	fmt.Printf("Creating bucket: %s\n", testBucket)
	if err := CreateBucketS3(ctx, testBucket); err != nil {
		log.Printf("Failed to create bucket: %v", err)
		return
	}

	// Create a hierarchical structure of objects
	objects := map[string]string{
		"folder1/file1.txt":           "content1",
		"folder1/file2.txt":           "content2",
		"folder1/subfolder/file3.txt": "content3",
		"folder2/file4.txt":           "content4",
		"folder2/file5.txt":           "content5",
		"rootfile1.txt":               "content6",
		"rootfile2.txt":               "content7",
	}

	// Upload all test objects
	fmt.Println("\nUploading test objects...")
	for key, content := range objects {
		if err := PutObjectS3(key, []byte(content), testBucket); err != nil {
			log.Printf("Failed to upload object %s: %v", key, err)
			continue
		}
	}

	// Test case 1: List all objects (no parameters)
	fmt.Println("\nTest Case 1: Listing all objects")
	input1 := &s3.ListObjectsV2Input{
		Bucket: aws.String(testBucket),
	}
	printListResults(ctx, input1)

	// Test case 2: List objects with prefix "folder1/"
	fmt.Println("\nTest Case 2: Listing objects with prefix 'folder1/'")
	input2 := &s3.ListObjectsV2Input{
		Bucket: aws.String(testBucket),
		Prefix: aws.String("folder1/"),
	}
	printListResults(ctx, input2)

	// Test case 3: List objects with delimiter "/"
	fmt.Println("\nTest Case 3: Listing with delimiter '/' (simulating directory listing)")
	input3 := &s3.ListObjectsV2Input{
		Bucket:    aws.String(testBucket),
		Delimiter: aws.String("/"),
	}
	printListResults(ctx, input3)

	// Test case 4: Combine prefix and delimiter
	fmt.Println("\nTest Case 4: Listing with prefix 'folder1/' and delimiter '/'")
	input4 := &s3.ListObjectsV2Input{
		Bucket:    aws.String(testBucket),
		Prefix:    aws.String("folder1/"),
		Delimiter: aws.String("/"),
	}
	printListResults(ctx, input4)

	// Test case 5: Use MaxKeys to paginate results
	fmt.Println("\nTest Case 5: Listing with MaxKeys=2 (pagination)")
	input5 := &s3.ListObjectsV2Input{
		Bucket:  aws.String(testBucket),
		MaxKeys: aws.Int32(2),
	}
	printListResults(ctx, input5)

	// Test case 6: Use StartAfter
	fmt.Println("\nTest Case 6: Listing objects starting after 'folder1/file1.txt'")
	input6 := &s3.ListObjectsV2Input{
		Bucket:     aws.String(testBucket),
		StartAfter: aws.String("folder1/file1.txt"),
	}
	printListResults(ctx, input6)

	// Clean up
	fmt.Println("\nCleaning up resources...")
	if err := DeleteBucketS3(ctx, testBucket); err != nil {
		log.Printf("Warning: Failed to delete bucket: %v", err)
	}
}

func Test6(ctx context.Context) {
	fmt.Println("\n=== Test: ListObjects Performance Test ===")
	testBucket := bucketName + "-listperf"
	numTestObjects := 10000
	batchSize := 1000

	// Create test bucket
	fmt.Printf("Creating bucket: %s\n", testBucket)
	if err := CreateBucketS3(ctx, testBucket); err != nil {
		log.Printf("Failed to create bucket: %v", err)
		return
	}

	// Create test objects
	fmt.Printf("Creating %d test objects...\n", numTestObjects)
	data := []byte("") // 0 byte of data
	createLatencies := make([]time.Duration, numTestObjects)
	for i := 0; i < numTestObjects; i++ {
		key := fmt.Sprintf("obj_%05d", i)
		start := time.Now()
		if err := PutObjectS3(key, data, testBucket); err != nil {
			log.Printf("Failed to create object %s: %v", key, err)
			continue
		}
		if i%1000 == 0 {
			fmt.Printf("Created %d objects...\n", i)
		}
		createLatencies[i] = time.Since(start)
	}
	calculateMetrics(createLatencies, "Object Creation")

	// Test listing performance
	fmt.Println("\nStarting listing operations...")
	listLatencies := make([]time.Duration, numTestObjects/batchSize)
	var continuationToken *string
	totalListStart := time.Now()

	for i := 0; i < len(listLatencies); i++ {
		input := &s3.ListObjectsV2Input{
			Bucket:  aws.String(testBucket),
			MaxKeys: aws.Int32(int32(batchSize)),
		}
		if continuationToken != nil {
			input.ContinuationToken = continuationToken
		}

		start := time.Now()
		output, err := s3Client.ListObjectsV2(ctx, input)
		listLatencies[i] = time.Since(start)

		if err != nil {
			log.Printf("Error listing objects (batch %d): %v", i, err)
			break
		}

		// Save the continuation token for next iteration
		continuationToken = output.NextContinuationToken
		if continuationToken == nil && i < len(listLatencies)-1 {
			log.Printf("No more objects to list after batch %d", i)
			listLatencies = listLatencies[:i+1] // Trim the latencies array
			break
		}
	}
	totalListTime := time.Since(totalListStart)
	fmt.Printf("\nTotal time to list all objects: %v\n", totalListTime)
	calculateMetrics(listLatencies, fmt.Sprintf("List (Batch Size: %d)", batchSize))

	// Delete all test objects
	fmt.Printf("Deleting %d test objects...\n", numTestObjects)
	deleteLatencies := make([]time.Duration, numTestObjects)
	for i := 0; i < numTestObjects; i++ {
		key := fmt.Sprintf("obj_%05d", i)
		start := time.Now()
		if err := DeleteObjectS3(key, testBucket); err != nil {
			log.Printf("Failed to delete object %s: %v", key, err)
			continue
		}
		if i%1000 == 0 {
			fmt.Printf("Created %d objects...\n", i)
		}
		deleteLatencies[i] = time.Since(start)
	}
	calculateMetrics(deleteLatencies, "Object Deletion")

	// Clean up
	fmt.Println("\nCleaning up resources...")
	if err := DeleteBucketS3(ctx, testBucket); err != nil {
		log.Printf("Warning: Failed to delete bucket: %v", err)
	}
}

func Test7(ctx context.Context) {
	fmt.Println("\n=== Test: Bucket Operations Performance Test ===")
	numTestBuckets := 1000
	testPrefix := bucketName + "-perf-"

	// Step 1: Create buckets and measure performance
	fmt.Printf("Creating %d test buckets...\n", numTestBuckets)
	createLatencies := make([]time.Duration, numTestBuckets)

	for i := 0; i < numTestBuckets; i++ {
		bucketName := fmt.Sprintf("%s%04d", testPrefix, i)
		start := time.Now()
		if err := CreateBucketS3(ctx, bucketName); err != nil {
			log.Printf("Failed to create bucket %s: %v", bucketName, err)
			continue
		}
		createLatencies[i] = time.Since(start)

		if i%100 == 0 {
			fmt.Printf("Created %d buckets...\n", i)
		}
	}

	calculateMetrics(createLatencies, "Bucket Creation")

	// Step 2: List buckets and measure performance (single operation)
	fmt.Println("\nTesting bucket listing performance...")
	start := time.Now()
	buckets, err := ListBucketsS3(ctx)
	listLatency := time.Since(start)
	if err != nil {
		log.Printf("Failed to list buckets: %v", err)
	} else {
		fmt.Printf("\nBucket Listing Metrics:\n")
		fmt.Printf("Total Latency: %v\n", listLatency)
		fmt.Printf("Listed %d buckets\n", len(buckets))
	}

	// Step 3: Delete buckets and measure performance
	fmt.Printf("\nDeleting %d test buckets...\n", numTestBuckets)
	deleteLatencies := make([]time.Duration, numTestBuckets)

	for i := 0; i < numTestBuckets; i++ {
		bucketName := fmt.Sprintf("%s%04d", testPrefix, i)
		start := time.Now()
		if err := DeleteBucketS3(ctx, bucketName); err != nil {
			log.Printf("Failed to delete bucket %s: %v", bucketName, err)
			continue
		}
		deleteLatencies[i] = time.Since(start)

		if i%100 == 0 {
			fmt.Printf("Deleted %d buckets...\n", i)
		}
	}

	calculateMetrics(deleteLatencies, "Bucket Deletion")
}

func Test8(ctx context.Context) {
	fmt.Println("\n=== Test 8: Large File (10GB) Upload/Download Performance Test ===")
	testBucket := bucketName + "-large"
	fileSize := int64(10 * 1024 * 1024 * 1024) // 10 GB
	key := "large-test-file"
	chunkSize := int64(100 * 1024 * 1024) // 100 MB chunks for multipart upload

	// Create test bucket
	fmt.Printf("Creating bucket: %s\n", testBucket)
	if err := CreateBucketS3(ctx, testBucket); err != nil {
		log.Printf("Failed to create bucket: %v", err)
		return
	}

	// Step 1: Generate test data
	fmt.Printf("Generating %d GB of test data...\n", fileSize/(1024*1024*1024))
	generateStart := time.Now()

	// Create a buffer that generates random data on read
	randomDataReader := &RandomDataReader{
		Size: fileSize,
	}

	generateTime := time.Since(generateStart)
	fmt.Printf("Data generation preparation time: %v\n", generateTime)

	// Step 2: Upload file using multipart upload
	fmt.Println("\nStarting multipart upload...")
	uploadStart := time.Now()

	// Initialize multipart upload
	createInput := &s3.CreateMultipartUploadInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
	}

	createOutput, err := s3Client.CreateMultipartUpload(ctx, createInput)
	if err != nil {
		log.Printf("Failed to create multipart upload: %v", err)
		return
	}

	// Calculate number of parts
	numParts := (fileSize + chunkSize - 1) / chunkSize
	var completedParts []types.CompletedPart

	fmt.Printf("Uploading file in %d parts of %d MB each...\n", numParts, chunkSize/(1024*1024))

	// Upload each part
	for partNum := int64(1); partNum <= numParts; partNum++ {
		start := (partNum - 1) * chunkSize
		size := min64(chunkSize, fileSize-start)

		// Create a limited reader for this chunk
		partReader := io.LimitReader(randomDataReader, size)

		uploadInput := &s3.UploadPartInput{
			Bucket:        aws.String(testBucket),
			Key:           aws.String(key),
			PartNumber:    aws.Int32(int32(partNum)),
			UploadId:      createOutput.UploadId,
			Body:          partReader,
			ContentLength: aws.Int64(size),
		}

		uploadOutput, err := s3Client.UploadPart(ctx, uploadInput)
		if err != nil {
			log.Printf("Failed to upload part %d: %v", partNum, err)
			// Abort upload
			_, _ = s3Client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
				Bucket:   aws.String(testBucket),
				Key:      aws.String(key),
				UploadId: createOutput.UploadId,
			})
			return
		}

		partNumber := int32(partNum)
		completedParts = append(completedParts, types.CompletedPart{
			ETag:       uploadOutput.ETag,
			PartNumber: &partNumber,
		})

		fmt.Printf("Uploaded part %d of %d\n", partNum, numParts)
	}

	// Complete multipart upload
	completeInput := &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(testBucket),
		Key:      aws.String(key),
		UploadId: createOutput.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
	}

	_, err = s3Client.CompleteMultipartUpload(ctx, completeInput)
	if err != nil {
		log.Printf("Failed to complete multipart upload: %v", err)
		return
	}

	uploadTime := time.Since(uploadStart)
	uploadSpeedMBps := float64(fileSize) / 1024 / 1024 / uploadTime.Seconds()
	fmt.Printf("Upload completed in %v (%.2f MB/s)\n", uploadTime, uploadSpeedMBps)

	// Step 3: Download file
	fmt.Println("\nStarting download...")
	downloadStart := time.Now()

	// Download the file in chunks to avoid memory issues
	downloadInput := &s3.GetObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
	}

	result, err := s3Client.GetObject(ctx, downloadInput)
	if err != nil {
		log.Printf("Failed to start download: %v", err)
		return
	}
	defer result.Body.Close()

	// Read and discard the data (we're just measuring download speed)
	var totalRead int64
	buffer := make([]byte, 1024*1024) // 1MB buffer
	for {
		n, err := result.Body.Read(buffer)
		if err != nil && err != io.EOF {
			log.Printf("Error during download: %v", err)
			return
		}
		totalRead += int64(n)
		if err == io.EOF {
			break
		}
	}

	downloadTime := time.Since(downloadStart)
	downloadSpeedMBps := float64(totalRead) / 1024 / 1024 / downloadTime.Seconds()
	fmt.Printf("Download completed in %v (%.2f MB/s)\n", downloadTime, downloadSpeedMBps)

	// Print summary
	fmt.Println("\nPerformance Summary:")
	fmt.Printf("File Size: %d GB\n", fileSize/(1024*1024*1024))
	fmt.Printf("Upload Time: %v (%.2f MB/s)\n", uploadTime, uploadSpeedMBps)
	fmt.Printf("Download Time: %v (%.2f MB/s)\n", downloadTime, downloadSpeedMBps)

	// Clean up
	fmt.Println("\nCleaning up resources...")
	if err := DeleteBucketS3(ctx, testBucket); err != nil {
		log.Printf("Warning: Failed to delete bucket: %v", err)
	}
}

// RandomDataReader generates random data on the fly to avoid storing large amounts in memory
type RandomDataReader struct {
	Size    int64
	Current int64
}

func (r *RandomDataReader) Read(p []byte) (n int, err error) {
	if r.Current >= r.Size {
		return 0, io.EOF
	}

	remaining := r.Size - r.Current
	toRead := int64(len(p))
	if remaining < toRead {
		toRead = remaining
	}

	// Fill the buffer with random data
	rand.Read(p[:toRead])

	r.Current += toRead
	return int(toRead), nil
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func printListResults(ctx context.Context, input *s3.ListObjectsV2Input) {
	paginator := s3.NewListObjectsV2Paginator(s3Client, input)
	pageNum := 0

	for paginator.HasMorePages() {
		pageNum++
		output, err := paginator.NextPage(ctx)
		if err != nil {
			log.Printf("Failed to list objects: %v", err)
			return
		}

		fmt.Printf("Page %d:\n", pageNum)
		if len(output.CommonPrefixes) > 0 {
			fmt.Println("Common Prefixes (folders):")
			for _, prefix := range output.CommonPrefixes {
				fmt.Printf("  - %s\n", *prefix.Prefix)
			}
		}

		if len(output.Contents) > 0 {
			fmt.Println("Objects:")
			for _, object := range output.Contents {
				fmt.Printf("  - %s (size: %d bytes)\n", *object.Key, object.Size)
			}
		}

		if output.NextContinuationToken != nil {
			fmt.Printf("Next Continuation Token: %s\n", *output.NextContinuationToken)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func ObjectAPIsTest(bucket string) {
	// Step 1: Write objects of varying sizes to S3
	fmt.Println("Starting write operations for varying object sizes...")
	for _, size := range objectSizes {
		writeLatencies := make([]time.Duration, numObjects)

		for i := 0; i < numObjects; i++ {
			key := fmt.Sprintf("naive_key_%d_size_%d", i, size)
			data := make([]byte, size)
			rand.Read(data)

			start := time.Now()
			if err := PutObjectS3(key, data, bucket); err != nil {
				log.Fatalf("Failed to put object to S3: %v", err)
			}
			writeLatencies[i] = time.Since(start)
		}

		calculateMetrics(writeLatencies, fmt.Sprintf("Write (Size: %d bytes)", size))
	}

	// Step 2: Read objects from S3 in order
	fmt.Println("\nStarting read operations for varying object sizes...")
	for _, size := range objectSizes {
		readLatencies := make([]time.Duration, numObjects)

		for i := 0; i < numObjects; i++ {
			key := fmt.Sprintf("naive_key_%d_size_%d", i, size)

			start := time.Now()
			if _, err := GetObjectS3(key, bucket); err != nil {
				log.Fatalf("Failed to get object from S3: %v", err)
			}
			readLatencies[i] = time.Since(start)
		}

		calculateMetrics(readLatencies, fmt.Sprintf("Read (Size: %d bytes)", size))
	}

	// Step 3: Delete all objects from S3
	fmt.Println("\nStarting delete operations for all objects...")
	for _, size := range objectSizes {
		deleteLatencies := make([]time.Duration, numObjects)

		for i := 0; i < numObjects; i++ {
			key := fmt.Sprintf("naive_key_%d_size_%d", i, size)
			start := time.Now()
			if err := DeleteObjectS3(key, bucket); err != nil {
				log.Printf("Failed to delete object: %v", err)
			} else {
				deleteLatencies[i] = time.Since(start)
			}
		}

		calculateMetrics(deleteLatencies, fmt.Sprintf("Delete (Size: %d bytes)", size))
	}
}

// ListBuckets lists the buckets in the current account.
func ListBucketsS3(ctx context.Context) ([]types.Bucket, error) {
	var err error
	var output *s3.ListBucketsOutput
	var buckets []types.Bucket
	bucketPaginator := s3.NewListBucketsPaginator(s3Client, &s3.ListBucketsInput{})
	for bucketPaginator.HasMorePages() {
		output, err = bucketPaginator.NextPage(ctx)
		if err != nil {
			var apiErr smithy.APIError
			if errors.As(err, &apiErr) && apiErr.ErrorCode() == "AccessDenied" {
				fmt.Println("You don't have permission to list buckets for this account.")
				err = apiErr
			} else {
				log.Printf("Couldn't list buckets for your account. Here's why: %v\n", err)
			}
			break
		} else {
			buckets = append(buckets, output.Buckets...)
		}
	}
	return buckets, err
}

// DeleteObjects deletes a list of objects from a bucket.
func DeleteObjectsS3(ctx context.Context, bucket string, objects []types.ObjectIdentifier, bypassGovernance bool) error {
	if len(objects) == 0 {
		return nil
	}
	input := s3.DeleteObjectsInput{
		Bucket: aws.String(bucket),
		Delete: &types.Delete{
			Objects: objects,
			Quiet:   aws.Bool(true),
		},
	}
	if bypassGovernance {
		input.BypassGovernanceRetention = aws.Bool(true)
	}
	delOut, err := s3Client.DeleteObjects(ctx, &input)
	if err != nil || len(delOut.Errors) > 0 {
		log.Printf("Error deleting objects from bucket %s.\n", bucket)
		if err != nil {
			var noBucket *types.NoSuchBucket
			if errors.As(err, &noBucket) {
				log.Printf("Bucket %s does not exist.\n", bucket)
				err = noBucket
			}
		} else if len(delOut.Errors) > 0 {
			for _, outErr := range delOut.Errors {
				log.Printf("%s: %s\n", *outErr.Key, *outErr.Message)
			}
			err = fmt.Errorf("%s", *delOut.Errors[0].Message)
		}
	} else {
		for _, delObjs := range delOut.Deleted {
			err = s3.NewObjectNotExistsWaiter(s3Client).Wait(
				ctx, &s3.HeadObjectInput{Bucket: aws.String(bucket), Key: delObjs.Key}, time.Minute)
			if err != nil {
				log.Printf("Failed attempt to wait for object %s to be deleted.\n", *delObjs.Key)
			} else {
				log.Printf("Deleted %s.\n", *delObjs.Key)
			}
		}
	}
	return err
}

// CopyToBucket copies an object in a bucket to another bucket.
func CopyToBucketS3(ctx context.Context, sourceBucket string, destinationBucket string, objectKey string) error {
	_, err := s3Client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(destinationBucket),
		CopySource: aws.String(fmt.Sprintf("%v/%v", sourceBucket, objectKey)),
		Key:        aws.String(objectKey),
	})
	if err != nil {
		var notActive *types.ObjectNotInActiveTierError
		if errors.As(err, &notActive) {
			log.Printf("Couldn't copy object %s from %s because the object isn't in the active tier.\n",
				objectKey, sourceBucket)
			err = notActive
		}
	} else {
		err = s3.NewObjectExistsWaiter(s3Client).Wait(
			ctx, &s3.HeadObjectInput{Bucket: aws.String(destinationBucket), Key: aws.String(objectKey)}, 30*time.Second)
		if err != nil {
			log.Printf("Failed attempt to wait for object %s to exist.\n", objectKey)
		}
	}
	return err
}

func CreateBucketS3(ctx context.Context, bucketName string) error {
	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucketName)})
	if err != nil {
		return fmt.Errorf("unable to create bucket %s: %v", bucketName, err)
	}

	// Wait until bucket is created
	waiter := s3.NewBucketExistsWaiter(s3Client)
	if err := waiter.Wait(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	}, 30*time.Second); err != nil {
		return fmt.Errorf("timeout while waiting for bucket creation: %v", err)
	}

	return nil
}

func DeleteBucketS3(ctx context.Context, bucketName string) error {
	// Delete all objects in bucket first
	listObjectsResp, err := s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		return fmt.Errorf("unable to list objects: %v", err)
	}

	// Convert []types.Object to []types.ObjectIdentifier
	objectIds := make([]types.ObjectIdentifier, len(listObjectsResp.Contents))
	for i, object := range listObjectsResp.Contents {
		objectIds[i] = types.ObjectIdentifier{Key: object.Key}
	}

	if err := DeleteObjectsS3(ctx, bucketName, objectIds, false); err != nil {
		return fmt.Errorf("unable to delete objects in bucket %s: %v", bucketName, err)
	}

	// Now delete the bucket
	_, err = s3Client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		return fmt.Errorf("unable to delete bucket %s: %v", bucketName, err)
	}

	return nil
}

// ListObjects lists the objects in a bucket.
func ListObjectsS3(ctx context.Context, bucketName string) ([]types.Object, error) {
	var err error
	var output *s3.ListObjectsV2Output
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	}
	var objects []types.Object
	objectPaginator := s3.NewListObjectsV2Paginator(s3Client, input)
	for objectPaginator.HasMorePages() {
		output, err = objectPaginator.NextPage(ctx)
		if err != nil {
			var noBucket *types.NoSuchBucket
			if errors.As(err, &noBucket) {
				log.Printf("Bucket %s does not exist.\n", bucketName)
				err = noBucket
			}
			break
		} else {
			objects = append(objects, output.Contents...)
		}
	}
	return objects, err
}

func PutObjectS3(key string, data []byte, bucketName string) error {
	input := &s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	}
	_, err := s3Client.PutObject(context.TODO(), input)
	return err
}

func GetObjectS3(key string, bucketName string) ([]byte, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
	}
	result, err := s3Client.GetObject(context.TODO(), input)
	if err != nil {
		return nil, err
	}
	defer result.Body.Close()

	return io.ReadAll(result.Body)
}

func DeleteObjectS3(key string, bucketName string) error {
	input := &s3.DeleteObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
	}
	_, err := s3Client.DeleteObject(context.TODO(), input)
	return err
}

func GetObjectMetadataS3(ctx context.Context, bucketName string, key string) (*s3.HeadObjectOutput, error) {
	input := &s3.HeadObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
	}
	return s3Client.HeadObject(ctx, input)
}

func calculateMetrics(latencies []time.Duration, operation string) {
	var totalLatency time.Duration
	for _, latency := range latencies {
		totalLatency += latency
	}

	avgLatency := totalLatency / time.Duration(len(latencies))
	throughput := float64(len(latencies)) / totalLatency.Seconds()

	fmt.Printf("\n%s Metrics:\n", operation)
	fmt.Printf("Average Latency: %v\n", avgLatency)
	fmt.Printf("Throughput: %.2f ops/sec\n", throughput)
}
