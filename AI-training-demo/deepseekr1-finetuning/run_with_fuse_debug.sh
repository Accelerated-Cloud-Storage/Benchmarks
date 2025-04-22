#!/bin/bash

# Set up directories for logs
LOG_DIR="debug_logs"
mkdir -p $LOG_DIR

# Set up strace to capture all filesystem and memory operations
STRACE_LOG="$LOG_DIR/strace_fs_operations.log"
echo "Starting strace to capture filesystem operations in $STRACE_LOG"

# Run the training script with enhanced strace to capture all relevant syscalls
strace -f -tt -T \
    -e trace=file,desc,network,memory,process \
    -e verbose=all \
    -s 2048 \
    -o $STRACE_LOG python train.py \
    --output_dir /mnt/acs-bucket/hf8 \
    --cache_dir /mnt/acs-bucket/hf8 \
    --dataset_name wikitext \
    --dataset_config wikitext-2-v1 \
    --max_train_samples 1000 \
    "$@"

# Process and analyze the strace log with enhanced analysis
echo "Analyzing filesystem operations..."

# Create a detailed analysis file
ANALYSIS_FILE="$LOG_DIR/fs_analysis.txt"
{
    echo "=== Filesystem Operations Analysis ==="
    echo -e "\nTop files accessed (with timing):"
    grep "open(" $STRACE_LOG | grep -v "= -1" | sort | uniq -c | sort -nr | head -50

    echo -e "\nTop directories accessed:"
    grep "opendir(" $STRACE_LOG | sort | uniq -c | sort -nr | head -20

    echo -e "\nSlowest filesystem operations (>0.1s):"
    grep -E "[0-9]+\.[0-9]{6}" $STRACE_LOG | awk '{if ($NF > 0.1) print $0}' | sort -k${NF} -nr | head -20

    echo -e "\nFile operations summary by type:"
    grep -E 'open|read|write|stat|unlink|rename|mkdir|rmdir|mmap|munmap' $STRACE_LOG | \
        awk '{print $2}' | sort | uniq -c | sort -nr

    echo -e "\nFailed operations (errors):"
    grep "= -1 E" $STRACE_LOG | sort | uniq -c | sort -nr

    echo -e "\nMemory mapping operations:"
    grep "mmap" $STRACE_LOG | sort | uniq -c | sort -nr | head -20
} > $ANALYSIS_FILE

echo "=== Debug Information ==="
echo "- Main log file: hf_training_debug.log"
echo "- Strace filesystem operations: $STRACE_LOG"
echo "- Filesystem operation analysis: $ANALYSIS_FILE"
echo "- Individual snapshot logs: $LOG_DIR/fs_debug_*.log"
echo "=== Analysis Summary ==="
echo "- Total filesystem operations: $(wc -l < $STRACE_LOG)"
echo "- Unique files accessed: $(grep "open(" $STRACE_LOG | grep -v "= -1" | sort -u | wc -l)"
echo "- Failed operations: $(grep "= -1 E" $STRACE_LOG | wc -l)"
echo "===========================" 