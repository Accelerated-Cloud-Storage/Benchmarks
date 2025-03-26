import os
import time
import random
import boto3
from botocore.exceptions import ClientError

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
    s3_client = boto3.client('s3', region_name='us-east-1')
    
    try:
        # Create test bucket
        bucket_name = f"large-object-test-{int(time.time_ns())}"
        print(f"\nCreating bucket: {bucket_name}")
        s3_client.create_bucket(Bucket=bucket_name)
        
        try:
            # Generate 10GB of random data
            object_size = 10 * 1024 * 1024 * 1024  # 10GB in bytes
            print(f"\nGenerating {object_size/(1024*1024*1024):.2f}GB of random data...")
            data = os.urandom(object_size)
            key = "large-object"
            
            # Upload large object using multipart upload
            print("\nUploading large object using multipart upload...")
            start = time.time()

            # Initialize multipart upload
            mpu = s3_client.create_multipart_upload(Bucket=bucket_name, Key=key)
            
            # Split data into chunks of 100MB
            chunk_size = 100 * 1024 * 1024  # 100MB chunks
            completed_parts = []
            
            for i, start_idx in enumerate(range(0, len(data), chunk_size), 1):
                end_idx = min(start_idx + chunk_size, len(data))
                chunk = data[start_idx:end_idx]
                
                # Upload part
                try:
                    part = s3_client.upload_part(
                        Bucket=bucket_name,
                        Key=key,
                        PartNumber=i,
                        UploadId=mpu['UploadId'],
                        Body=chunk
                    )
                    completed_parts.append({
                        'PartNumber': i,
                        'ETag': part['ETag']
                    })
                    print(f"Uploaded part {i}")
                except Exception as e:
                    print(f"Failed to upload part {i}: {e}")
                    # Abort multipart upload on failure
                    s3_client.abort_multipart_upload(
                        Bucket=bucket_name,
                        Key=key,
                        UploadId=mpu['UploadId']
                    )
                    raise
            
            # Complete multipart upload
            s3_client.complete_multipart_upload(
                Bucket=bucket_name,
                Key=key,
                UploadId=mpu['UploadId'],
                MultipartUpload={'Parts': completed_parts}
            )
            
            upload_latency = time.time() - start
            calculate_metrics([upload_latency], "Large Object Upload (Multipart)", object_size)
            
            # Read large object
            print("\nReading large object...")
            start = time.time()
            response = s3_client.get_object(
                Bucket=bucket_name,
                Key=key
            )
            # Read all data to fully complete the operation
            retrieved_data = response['Body'].read()
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
            s3_client.delete_object(
                Bucket=bucket_name,
                Key=key
            )
            delete_latency = time.time() - start
            calculate_metrics([delete_latency], "Large Object Deletion", object_size)
            
        finally:
            # Clean up - delete bucket
            print(f"\nCleaning up bucket: {bucket_name}")
            s3_client.delete_bucket(Bucket=bucket_name)
            
    except ClientError as e:
        print(f"S3 operation error: {e}")

def list_operations_test():
    """Test bucket and object listing operations."""
    print("\n===== LIST OPERATIONS TEST =====")
    
    # Initialize client
    s3_client = boto3.client('s3', region_name='us-east-1')
    
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
                s3_client.create_bucket(Bucket=bucket_name)
                bucket_create_latencies.append(time.time() - start)
            except ClientError as e:
                print(f"Failed to create bucket {bucket_name}: {e}")
        
        calculate_metrics(bucket_create_latencies, "Bucket Creation")
        
        # List all buckets
        print("\nListing all buckets...")
        list_bucket_latencies = []
        
        for _ in range(10):  # Perform list operation 10 times for more reliable metrics
            start = time.time()
            try:
                s3_client.list_buckets()
                list_bucket_latencies.append(time.time() - start)
            except ClientError as e:
                print(f"Failed to list buckets: {e}")
        
        calculate_metrics(list_bucket_latencies, "Bucket Listing")
        
        # Delete all buckets
        print(f"\nDeleting {num_buckets} buckets...")
        bucket_delete_latencies = []
        
        for bucket_name in bucket_names:
            start = time.time()
            try:
                s3_client.delete_bucket(Bucket=bucket_name)
                bucket_delete_latencies.append(time.time() - start)
            except ClientError as e:
                print(f"Failed to delete bucket {bucket_name}: {e}")
        
        calculate_metrics(bucket_delete_latencies, "Bucket Deletion")
        
        # Part 2: Object List Test
        object_test_bucket = f"object-list-test-{int(time.time_ns())}"
        num_objects = 1000
        
        # Create a bucket for object tests
        print(f"\nCreating bucket for object list test: {object_test_bucket}")
        s3_client.create_bucket(Bucket=object_test_bucket)
        
        try:
            # Create 1000 small objects
            print(f"\nCreating {num_objects} objects of size 1 byte...")
            object_create_latencies = []
            data = b"0"  # 1 byte of data
            
            for i in range(num_objects):
                key = f"small-object-{i}"
                
                start = time.time()
                try:
                    s3_client.put_object(
                        Bucket=object_test_bucket,
                        Key=key,
                        Body=data
                    )
                    object_create_latencies.append(time.time() - start)
                except ClientError as e:
                    print(f"Failed to put object {key}: {e}")
            
            calculate_metrics(object_create_latencies, "Object Creation", 1)
            
            # List all objects
            print("\nListing all objects...")
            list_object_latencies = []
            
            for _ in range(10):  # Perform list operation 10 times
                start = time.time()
                try:
                    s3_client.list_objects_v2(Bucket=object_test_bucket)
                    list_object_latencies.append(time.time() - start)
                except ClientError as e:
                    print(f"Failed to list objects: {e}")
            
            calculate_metrics(list_object_latencies, "Object Listing")
            
            # Delete all objects
            print(f"\nDeleting {num_objects} objects...")
            object_delete_latencies = []
            
            for i in range(num_objects):
                key = f"small-object-{i}"
                
                start = time.time()
                try:
                    s3_client.delete_object(
                        Bucket=object_test_bucket,
                        Key=key
                    )
                    object_delete_latencies.append(time.time() - start)
                except ClientError as e:
                    print(f"Failed to delete object {key}: {e}")
            
            calculate_metrics(object_delete_latencies, "Object Deletion", 1)
            
        finally:
            # Clean up - delete bucket
            print(f"\nCleaning up bucket: {object_test_bucket}")
            s3_client.delete_bucket(Bucket=object_test_bucket)
            
    except ClientError as e:
        print(f"S3 operation error: {e}")

def main():
    print("AWS S3 SDK Benchmark - Test Suite 2")
    print("===================================")
    
    large_object_test()
    list_operations_test()

if __name__ == "__main__":
    main() 