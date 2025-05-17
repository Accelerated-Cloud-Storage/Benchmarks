import os
import time
import random
from google.cloud import storage
from google.api_core import exceptions
from google.cloud.storage import Blob

def percentile(values, perc):
    """Calculate percentile of a sorted list of values."""
    sorted_values = sorted(values)
    k = (len(sorted_values) - 1) * perc
    f = int(k)
    c = int(k) + 1 if k < len(sorted_values) - 1 else int(k)
    d0 = sorted_values[f] * (c - k)
    d1 = sorted_values[c] * (k - f)
    return d0 + d1

def calculate_metrics(latencies, operation, data_size=0):
    """Calculate and print metrics for operations."""
    if not latencies:
        print(f"No valid latencies for {operation}")
        return
        
    # Sort latencies for percentile calculations
    sorted_latencies = sorted(latencies)
    
    # Calculate basic statistics
    min_latency = sorted_latencies[0]
    avg_latency = sum(latencies) / len(latencies)
    
    # Calculate throughput in ops/sec
    total_time = sum(latencies)
    ops_per_sec = len(latencies) / total_time if total_time > 0 else 0
    
    # Calculate throughput in GB/sec if data_size is provided
    gb_per_sec = 0
    if data_size > 0:
        total_bytes = data_size * len(latencies)
        gb_per_sec = (total_bytes / total_time) / (1024 * 1024 * 1024) if total_time > 0 else 0
    
    # Calculate percentiles
    p90 = percentile(sorted_latencies, 0.90)
    p95 = percentile(sorted_latencies, 0.95)
    p99 = percentile(sorted_latencies, 0.99)
    
    # Convert latencies from seconds to milliseconds
    min_ms = min_latency * 1000
    avg_ms = avg_latency * 1000
    p90_ms = p90 * 1000
    p95_ms = p95 * 1000
    p99_ms = p99 * 1000
    
    print(f"\n{operation} Metrics:")
    print(f"Min Latency: {min_ms:.2f} ms")
    print(f"Average Latency: {avg_ms:.2f} ms")
    print(f"P90 Latency: {p90_ms:.2f} ms")
    print(f"P95 Latency: {p95_ms:.2f} ms")
    print(f"P99 Latency: {p99_ms:.2f} ms")
    print(f"Throughput: {ops_per_sec:.2f} ops/sec")
    if data_size > 0:
        print(f"Throughput: {gb_per_sec:.6f} GB/sec")

def large_object_test():
    """Test operations with a large 10GB object."""
    print("\n===== LARGE OBJECT TEST =====")
    
    # Initialize client
    storage_client = storage.Client()
    
    try:
        # Create test bucket
        bucket_name = f"large-object-test-{int(time.time_ns())}"
        print(f"\nCreating bucket: {bucket_name}")
        bucket = storage_client.create_bucket(bucket_name)
        
        try:
            # Generate 10GB of random data
            object_size = 10 * 1024 * 1024 * 1024  # 10GB in bytes
            print(f"\nGenerating {object_size/(1024*1024*1024):.2f}GB of random data...")
            data = os.urandom(object_size)
            blob_name = "large-object"
            
            # Upload large object using resumable upload
            print("\nUploading large object using resumable upload...")
            start = time.time()
            
            blob = bucket.blob(blob_name)
            blob.chunk_size = 100 * 1024 * 1024  # 100MB chunks
            
            try:
                blob.upload_from_string(data)
                print("Upload completed successfully")
            except Exception as e:
                print(f"Failed to upload large object: {e}")
                raise
            
            upload_latency = time.time() - start
            calculate_metrics([upload_latency], "Large Object Upload (Resumable)", object_size)
            
            # Read large object
            print("\nReading large object...")
            start = time.time()
            try:
                retrieved_data = blob.download_as_bytes()
            except exceptions.NotFound as e:
                print(f"Failed to download object: {e}")
                raise
                
            download_latency = time.time() - start
            calculate_metrics([download_latency], "Large Object Download", object_size)
            
            # Verify data integrity
            print("\nVerifying data integrity...")
            if len(retrieved_data) != len(data):
                print(f"Data size mismatch! Original: {len(data)} bytes, Retrieved: {len(retrieved_data)} bytes")
            elif retrieved_data != data:
                print("Data content mismatch!")
            else:
                print("Data integrity verified successfully!")
            
            # Delete large object
            print("\nDeleting large object...")
            start = time.time()
            try:
                blob.delete()
            except exceptions.NotFound as e:
                print(f"Failed to delete object: {e}")
                
            delete_latency = time.time() - start
            calculate_metrics([delete_latency], "Large Object Deletion", object_size)
            
        finally:
            # Clean up - delete bucket
            print(f"\nCleaning up bucket: {bucket_name}")
            bucket.delete(force=True)
            
    except exceptions.GoogleAPIError as e:
        print(f"GCS operation error: {e}")

def list_operations_test():
    """Test bucket and object listing operations."""
    print("\n===== LIST OPERATIONS TEST =====")
    
    # Initialize client
    storage_client = storage.Client()
    
    try:
        # Part 1: Bucket List Test
        base_bucket_name = f"list-test-{int(time.time_ns())}"
        num_buckets = 100
        bucket_names = []
        
        # Create 100 buckets
        print(f"\nCreating {num_buckets} buckets...")
        bucket_create_latencies = []
        
        for i in range(num_buckets):
            bucket_name = f"{base_bucket_name}-{i}"
            bucket_names.append(bucket_name)
            
            start = time.time()
            try:
                storage_client.create_bucket(bucket_name)
                bucket_create_latencies.append(time.time() - start)
            except exceptions.Conflict as e:
                print(f"Failed to create bucket {bucket_name}: {e}")
        
        calculate_metrics(bucket_create_latencies, "Bucket Creation")
        
        # List all buckets
        print("\nListing all buckets...")
        list_bucket_latencies = []
        
        for _ in range(10):  # Perform list operation 10 times for more reliable metrics
            start = time.time()
            try:
                list(storage_client.list_buckets())
                list_bucket_latencies.append(time.time() - start)
            except exceptions.GoogleAPIError as e:
                print(f"Failed to list buckets: {e}")
        
        calculate_metrics(list_bucket_latencies, "Bucket Listing")
        
        # Delete all buckets
        print(f"\nDeleting {num_buckets} buckets...")
        bucket_delete_latencies = []
        
        for bucket_name in bucket_names:
            start = time.time()
            try:
                bucket = storage_client.bucket(bucket_name)
                bucket.delete()
                bucket_delete_latencies.append(time.time() - start)
            except exceptions.NotFound as e:
                print(f"Failed to delete bucket {bucket_name}: {e}")
        
        calculate_metrics(bucket_delete_latencies, "Bucket Deletion")
        
        # Part 2: Object List Test
        object_test_bucket = f"object-list-test-{int(time.time_ns())}"
        num_objects = 1000
        
        # Create a bucket for object tests
        print(f"\nCreating bucket for object list test: {object_test_bucket}")
        bucket = storage_client.create_bucket(object_test_bucket)
        
        try:
            # Create 1000 small objects
            print(f"\nCreating {num_objects} objects of size 1 byte...")
            object_create_latencies = []
            data = b"0"  # 1 byte of data
            
            for i in range(num_objects):
                blob_name = f"small-object-{i}"
                blob = bucket.blob(blob_name)
                
                start = time.time()
                try:
                    blob.upload_from_string(data)
                    object_create_latencies.append(time.time() - start)
                except exceptions.GoogleAPIError as e:
                    print(f"Failed to upload object {blob_name}: {e}")
            
            calculate_metrics(object_create_latencies, "Object Creation", 1)
            
            # List all objects
            print("\nListing all objects...")
            list_object_latencies = []
            
            for _ in range(10):  # Perform list operation 10 times
                start = time.time()
                try:
                    list(bucket.list_blobs())
                    list_object_latencies.append(time.time() - start)
                except exceptions.GoogleAPIError as e:
                    print(f"Failed to list objects: {e}")
            
            calculate_metrics(list_object_latencies, "Object Listing")
            
            # Delete all objects
            print(f"\nDeleting {num_objects} objects...")
            object_delete_latencies = []
            
            for i in range(num_objects):
                blob_name = f"small-object-{i}"
                blob = bucket.blob(blob_name)
                
                start = time.time()
                try:
                    blob.delete()
                    object_delete_latencies.append(time.time() - start)
                except exceptions.NotFound as e:
                    print(f"Failed to delete object {blob_name}: {e}")
            
            calculate_metrics(object_delete_latencies, "Object Deletion", 1)
            
        finally:
            # Clean up - delete bucket
            print(f"\nCleaning up bucket: {object_test_bucket}")
            bucket.delete(force=True)
            
    except exceptions.GoogleAPIError as e:
        print(f"GCS operation error: {e}")

def main():
    print("Google Cloud Storage SDK Benchmark - Test Suite 2")
    print("===============================================")
    
    large_object_test()
    list_operations_test()

if __name__ == "__main__":
    main()
