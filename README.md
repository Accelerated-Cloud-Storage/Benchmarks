# Benchmarks

Repository for comparing ACS object storage to other existing solutions.

## Directory Structure

The repository is organized into several test directories:

- `acs-client-test/`: Contains SDK client benchmarks for ACS Object Storage
  - `golang/`: Go SDK benchmarks
  - `python/`: Python SDK benchmarks
- `s3-client-test/`: Contains SDK client benchmarks for AWS S3
  - `golang/`: Go SDK benchmarks
  - `python/`: Python SDK benchmarks
- `fuse-mount-test/`: Contains FUSE filesystem performance tests
  - `benchmark.py`: Script for running filesystem performance comparisons
- `experimentResults/`: Directory where benchmark results are stored

## Prerequisites

This project requires both ACS and AWS Go/Python SDKs. Make sure to configure your ACS and AWS credentials before continuing. Please refer to those repositories if any questions arise.

### Setup Instructions

**Golang Setup**

```bash
go mod download
```

**Python Setup**

```bash
# Create a virtual environment
python -m venv .venv
source .venv/bin/activate  # On Windows: .venv\Scripts\activate
pip install -r requirements.txt 
```

## Mounting Object Storage Buckets

### Mountpoint for ACS Object Storage

1. **Prepare the mount point directory**

   ```bash
   sudo mkdir -p /mnt/acs-bucket
   sudo chown $USER:$USER /mnt/acs-bucket
   ```

2. **Mount the bucket**

   ```bash
   python -m acs_sdk.fuse YOUR-BUCKET-NAME /mnt/acs-bucket
   ```

3. **Verify the mount**

   ```bash
   ls -la /mnt/acs-bucket
   ```

### Mountpoint for Amazon S3

Download AWS Mountpoint-S3 FUSE mount: [https://github.com/awslabs/mountpoint-s3](https://github.com/awslabs/mountpoint-s3)

1. **Prepare the mount point directory**

   ```bash
   sudo mkdir -p /mnt/aws-bucket
   sudo chown $USER:$USER /mnt/aws-bucket
   ```

2. **Mount the S3 bucket**

   ```bash
   mount-s3 YOUR-BUCKET-NAME /mnt/aws-bucket --allow-delete --allow-overwrite
   ```

   **Flags:**
   - `--allow-delete`: Permits deletion operations on the mounted filesystem
   - `--allow-overwrite`: Allows files to be modified/overwritten

3. **Enable access for other users (Optional)**

   Edit the FUSE configuration:

   ```bash
   echo "user_allow_other" | sudo tee -a /etc/fuse.conf
   ```

   Add the `--allow-other` flag when mounting:

   ```bash
   mount-s3 YOUR-BUCKET-NAME /mnt/aws-bucket --allow-delete --allow-overwrite --allow-other
   ```

4. **Verify the mount**

   ```bash
   ls -la /mnt/aws-bucket
   ```

## Running Benchmarks

### SDK Client Benchmarks

1. **ACS Client Tests**

   For Python:

   ```bash
   cd acs-client-test/python
   python TEST-FILE.py
   ```

   For Go:

   ```bash
   cd acs-client-test/golang
   go run TEST-FILE.go
   ```

2. **S3 Client Tests**

   For Python:

   ```bash
   cd s3-client-test/python
   python TEST-FILE.py 
   ```

   For Go:

   ```bash
   cd s3-client-test/golang
   go run TEST-FILE.go
   ```

### FUSE Mount Performance Tests

To run filesystem performance comparisons between mounted ACS and S3 buckets:

```bash
cd fuse-mount-test
python benchmark.py YOUR-MOUNT-POINT
```

The benchmark results have been saved in the `experimentResults/` directory.

**Note**: Make sure you have mounted both the ACS and S3 buckets as described in the mounting instructions before running the FUSE performance tests.
