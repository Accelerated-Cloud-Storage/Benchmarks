# Benchmarks and Demos

Repository for comparing ACS object storage to other existing solutions.

## Directory Structure

The repository is organized into several test directories:

- `acs-client-test/`: Contains SDK client benchmarks for ACS Object Storage
  - `golang/`: Go SDK benchmarks
  - `python/`: Python SDK benchmarks
- `s3-client-test/`: Contains SDK client benchmarks for AWS S3
  - `golang/`: Go SDK benchmarks
  - `python/`: Python SDK benchmarks
- `gcs-client-test/`: Contains SDK client benchmarks for Google Cloud Storage
- `tigris-client-test/`: Contains SDK client benchmarks for Tigris
- `fuse-mount-test/`: Contains FUSE filesystem performance tests
  - `benchmark.py`: Script for running filesystem performance comparisons
- `batch-api-demo/`: Contains batch API demonstration examples
- `experimentResults/`: Directory where benchmark results are stored

## Prerequisites

This project requires the following SDKs and credentials:

- ACS SDK and credentials
- AWS SDK and credentials
- Google Cloud Storage SDK and credentials
- Tigris SDK and credentials

Please ensure you have configured the credentials for each service before running the benchmarks. Refer to the respective documentation for setup instructions:

- [ACS Documentation](https://docs.acs.com)
- [AWS Documentation](https://docs.aws.amazon.com/sdk-for-go/v1/developer-guide/setting-up.html)
- [Google Cloud Documentation](https://cloud.google.com/storage/docs/reference/libraries)
- [Tigris Documentation](https://www.tigrisdata.com/docs/)

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

# Install dependencies
pip install -r requirements.txt
```

The Python environment includes the following main packages:
- ACS SDK (acs-sdk >= 0.3.2)
- AWS SDK (boto3)
- Google Cloud Storage SDK
- FUSE filesystem support
- Additional utilities and dependencies

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

3. **Google Cloud Storage Tests**

   ```bash
   cd gcs-client-test
   python TEST-FILE.py
   ```

4. **Tigris Client Tests**

   ```bash
   cd tigris-client-test
   python TEST-FILE.py
   ```

### FUSE Mount Performance Tests

To run filesystem performance comparisons between mounted storage buckets:

```bash
cd fuse-mount-test
python benchmark.py YOUR-MOUNT-POINT
```

The benchmark results will be saved in the `experimentResults/` directory.

**Note**: Make sure you have mounted the respective storage buckets as described in the mounting instructions before running the FUSE performance tests.
