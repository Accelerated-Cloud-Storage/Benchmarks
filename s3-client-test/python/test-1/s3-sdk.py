import os
import time
import random
import boto3
from botocore.exceptions import ClientError

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
    # Initialize S3 client
    s3_client = boto3.client('s3', region_name='us-east-1')
    
    try:
        # Create a unique bucket name
        bucket = f"test-bucket-{int(time.time_ns())}"
        
        # Create bucket
        print(f"Creating bucket: {bucket}")
        s3_client.create_bucket(Bucket=bucket)
        
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
                    key = f"key_{i}_size_{size}"
                    data = random.randbytes(size)
                    
                    start = time.time()
                    s3_client.put_object(
                        Bucket=bucket,
                        Key=key,
                        Body=data
                    )
                    write_latencies.append(time.time() - start)
                    
                calculate_metrics(write_latencies, f"Write (Size: {size} bytes)", size)

            # Step 2: Read objects
            print("\nStarting read operations for varying object sizes...")
            for size in object_sizes:
                print(f"\nReading {num_objects} objects of size {size} bytes")
                read_latencies = []
                
                for i in range(num_objects):
                    key = f"key_{i}_size_{size}"
                    
                    start = time.time()
                    try:
                        response = s3_client.get_object(
                            Bucket=bucket,
                            Key=key
                        )
                        # Read all data to fully complete the operation
                        response['Body'].read()
                    except ClientError as e:
                        print(f"Failed to get object {key}: {e}")
                        
                    read_latencies.append(time.time() - start)
                    
                calculate_metrics(read_latencies, f"Read (Size: {size} bytes)", size)

            # Step 3: Delete objects
            print("\nStarting delete operations...")
            for size in object_sizes:
                print(f"\nDeleting {num_objects} objects of size {size} bytes")
                delete_latencies = []
                
                for i in range(num_objects):
                    key = f"key_{i}_size_{size}"
                    
                    start = time.time()
                    try:
                        s3_client.delete_object(
                            Bucket=bucket,
                            Key=key
                        )
                    except ClientError as e:
                        print(f"Failed to delete object {key}: {e}")
                        
                    delete_latencies.append(time.time() - start)
                    
                calculate_metrics(delete_latencies, f"Delete (Size: {size} bytes)", size)

        finally:
            # Clean up - delete any remaining objects and bucket
            print(f"\nCleaning up bucket: {bucket}")
            try:
                # List and delete all objects
                response = s3_client.list_objects_v2(Bucket=bucket)
                if 'Contents' in response:
                    for obj in response['Contents']:
                        s3_client.delete_object(
                            Bucket=bucket,
                            Key=obj['Key']
                        )
                # Delete bucket
                s3_client.delete_bucket(Bucket=bucket)
            except ClientError as e:
                print(f"Error during cleanup: {e}")
            
    except ClientError as e:
        print(f"S3 operation error: {e}")

if __name__ == "__main__":
    main()
