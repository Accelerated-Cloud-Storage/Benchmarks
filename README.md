# Benchmarks

Repository for comparing ACS object storage to other existing solutions.

## Prerequisites

This project requires both ACS and AWS Go/Python SDKs. Make sure to configure your ACS and AWS credentials before continuing.

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
