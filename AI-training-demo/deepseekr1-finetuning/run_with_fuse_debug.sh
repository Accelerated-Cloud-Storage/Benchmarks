#!/bin/bash
# Set up strace to capture all filesystem operations
STRACE_LOG="strace_fs_operations.log"
echo "Starting strace to capture filesystem operations in $STRACE_LOG"

# Run the training script with strace to capture all filesystem calls
strace -f -e trace=file,desc -o $STRACE_LOG python train.py \
  --output_dir /mnt/acs-bucket/hf4 \
  --cache_dir /mnt/acs-bucket/hf4 \
  --dataset_name wikitext \
  --dataset_config wikitext-2-v1 \
  --max_train_samples 1000
  "$@"

# Process and analyze the strace log to summarize filesystem operations
echo "Analyzing filesystem operations..."
echo "Top files accessed:" > fs_analysis.txt
grep "open(" $STRACE_LOG | grep -v "= -1" | sort | uniq -c | sort -nr | head -50 >> fs_analysis.txt
echo -e "\nTop directories accessed:" >> fs_analysis.txt
grep "opendir(" $STRACE_LOG | sort | uniq -c | sort -nr | head -20 >> fs_analysis.txt
echo -e "\nFile operations summary:" >> fs_analysis.txt
grep -E 'open|read|write|stat|unlink|rename|mkdir|rmdir' $STRACE_LOG | awk '{print $2}' | sort | uniq -c | sort -nr >> fs_analysis.txt

echo "=== Debug Information ==="
echo "- Main log file: hf_training_debug.log"
echo "- Strace filesystem operations: $STRACE_LOG"
echo "- Filesystem operation analysis: fs_analysis.txt"
echo "- Individual snapshot logs: fs_debug_*.log"
echo "===========================" 