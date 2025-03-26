import boto3
from botocore.config import Config
from awscrt import auth, http
from awscrt.s3 import S3Client, S3RequestType
import time
import random
import os

# Configure the S3 client with CRT
config = Config(
    region_name='us-east-1',
    s3={
        'use_accelerate_endpoint': False,
        'addressing_style': 'path',
        'use_arn_region': False,
    }
)

s3_crt_client = boto3.client('s3', config=config)

# Define bucket and object details
bucket_name = 's3expressonezoneacs--use1-az6--x-s3'
num_objects = 10
object_sizes = [1024, 1024*1024, 10*1024*1024]

def put_object_s3(key, data):
    s3_crt_client.put_object(Bucket=bucket_name, Key=key, Body=data)

def get_object_s3(key):
    response = s3_crt_client.get_object(Bucket=bucket_name, Key=key)
    return response['Body'].read()

def delete_object_s3(key):
    s3_crt_client.delete_object(Bucket=bucket_name, Key=key)

def calculate_metrics(latencies, size_label):
    total = sum(latencies)
    avg = total / len(latencies)
    throughput = len(latencies) / ((total / 1000) or 1e-9)
    print(f"\n{size_label} Latencies:")
    for i, lat in enumerate(latencies):
        print(f"Request {i+1}: {lat:.2f} ms")
    print(f"Average {size_label} latency: {avg:.2f} ms")
    print(f"Overall {size_label} throughput: {throughput:.2f} req/s")

if __name__ == "__main__":
    for size in object_sizes:
        print(f"Writing {num_objects} objects of size {size} bytes...")
        write_latencies = []
        for i in range(num_objects):
            key = f"naive_key_{i}_size_{size}"
            data = os.urandom(size)
            start = time.time()
            put_object_s3(key, data)
            write_latencies.append((time.time() - start) * 1000)
        calculate_metrics(write_latencies, f"Write (Size: {size})")

    for size in object_sizes:
        print(f"\nReading {num_objects} objects of size {size} bytes...")
        read_latencies = []
        for i in range(num_objects):
            key = f"naive_key_{i}_size_{size}"
            start = time.time()
            get_object_s3(key)
            read_latencies.append((time.time() - start) * 1000)
        calculate_metrics(read_latencies, f"Read (Size: {size})")

    for size in object_sizes:
        print(f"\nDeleting {num_objects} objects of size {size} bytes...")
        delete_latencies = []
        for i in range(num_objects):
            key = f"naive_key_{i}_size_{size}"
            start = time.time()
            delete_object_s3(key)
            delete_latencies.append((time.time() - start) * 1000)
        calculate_metrics(delete_latencies, f"Delete (Size: {size})")
