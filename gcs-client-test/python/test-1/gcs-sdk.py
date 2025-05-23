import os
import time
import random
from google.cloud import storage
from google.api_core import exceptions

def percentile(values, perc):
    """Calculate percentile of a sorted list of values."""
    k = (len(values) - 1) * perc
    f = int(k)
    c = int(k) + 1 if k < len(values) - 1 else int(k)
    d0 = values[f] * (c - k)
    d1 = values[c] * (k - f)
    return d0 + d1

def calculate_metrics(latencies, operation, data_size=0):
    """Calculate and print metrics for operations."""
    # Sort latencies for percentile calculations
    sorted_latencies = sorted(latencies)
    
    # Calculate basic statistics
    min_latency = sorted_latencies[0]
    avg_latency = sum(latencies) / len(latencies)
    
    # Calculate throughput in ops/sec
    ops_per_sec = len(latencies) / sum(latencies)
    
    # Calculate throughput in GB/sec if data_size is provided
    if data_size > 0:
        total_bytes = data_size * len(latencies)
        gb_per_sec = (total_bytes / sum(latencies)) / (1024 * 1024 * 1024)
    
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
        print(f"Throughput: {gb_per_sec:.4f} GB/sec")

def main():
    # Initialize GCS client
    storage_client = storage.Client()
    
    try:
        # Create a unique bucket name
        bucket_name = f"test-bucket-{int(time.time_ns())}"
        
        # Create bucket
        print(f"Creating bucket: {bucket_name}")
        bucket = storage_client.create_bucket(bucket_name)
        
        try:
            # Define test object sizes
            object_sizes = [1024, 1024 * 1024, 10 * 1024 * 1024]  # 1KB, 1MB, 10MB
            num_objects = 50

            # Step 1: Write objects of varying sizes
            print("Starting write operations for varying object sizes...")
            for size in object_sizes:
                print(f"\nWriting {num_objects} objects of size {size} bytes")
                write_latencies = []
                
                for i in range(num_objects):
                    blob_name = f"blob_{i}_size_{size}"
                    blob = bucket.blob(blob_name)
                    data = random.randbytes(size)
                    
                    start = time.time()
                    blob.upload_from_string(data)
                    write_latencies.append(time.time() - start)
                    
                calculate_metrics(write_latencies, f"Write (Size: {size} bytes)", size)

            # Step 2: Read objects
            print("\nStarting read operations for varying object sizes...")
            for size in object_sizes:
                print(f"\nReading {num_objects} objects of size {size} bytes")
                read_latencies = []
                
                for i in range(num_objects):
                    blob_name = f"blob_{i}_size_{size}"
                    blob = bucket.blob(blob_name)
                    
                    start = time.time()
                    try:
                        # Read all data to fully complete the operation
                        blob.download_as_bytes()
                    except exceptions.NotFound as e:
                        print(f"Failed to get object {blob_name}: {e}")
                        
                    read_latencies.append(time.time() - start)
                    
                calculate_metrics(read_latencies, f"Read (Size: {size} bytes)", size)

            # Step 3: Delete objects
            print("\nStarting delete operations...")
            for size in object_sizes:
                print(f"\nDeleting {num_objects} objects of size {size} bytes")
                delete_latencies = []
                
                for i in range(num_objects):
                    blob_name = f"blob_{i}_size_{size}"
                    blob = bucket.blob(blob_name)
                    
                    start = time.time()
                    try:
                        blob.delete()
                    except exceptions.NotFound as e:
                        print(f"Failed to delete object {blob_name}: {e}")
                        
                    delete_latencies.append(time.time() - start)
                    
                calculate_metrics(delete_latencies, f"Delete (Size: {size} bytes)", size)

        finally:
            # Clean up - delete any remaining objects and bucket
            print(f"\nCleaning up bucket: {bucket_name}")
            try:
                blobs = bucket.list_blobs()
                for blob in blobs:
                    blob.delete()
                # Delete bucket
                bucket.delete()
            except exceptions.NotFound as e:
                print(f"Error during cleanup: {e}")
            
    except exceptions.GoogleAPIError as e:
        print(f"GCS operation error: {e}")

if __name__ == "__main__":
    main()
