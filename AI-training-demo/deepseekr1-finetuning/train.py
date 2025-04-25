import os
import logging

# Configure verbose logging for Hugging Face and filesystem operations
os.environ['TRANSFORMERS_VERBOSITY'] = 'debug'  # Set Transformers logging to debug level
os.environ['HF_DATASETS_VERBOSITY'] = 'debug'   # Set Datasets logging to debug level
os.environ['HF_HUB_VERBOSITY'] = 'debug'        # Set Hub logging to debug level

# Configure Python logging
logging.basicConfig(
    level=logging.DEBUG,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s',
    handlers=[
        logging.StreamHandler(),
        logging.FileHandler('hf_training_debug.log')
    ]
)

# Get specific loggers and set them to debug
huggingface_hub_logger = logging.getLogger("huggingface_hub")
huggingface_hub_logger.setLevel(logging.DEBUG)
transformers_logger = logging.getLogger("transformers")
transformers_logger.setLevel(logging.DEBUG)
datasets_logger = logging.getLogger("datasets")
datasets_logger.setLevel(logging.DEBUG)

import argparse
import torch
import time
from datasets import load_dataset
from transformers import (
    AutoTokenizer,
    AutoModelForCausalLM,
    TrainingArguments,
    Trainer,
    DataCollatorForLanguageModeling,
    TrainerCallback,
)


def parse_args():
    parser = argparse.ArgumentParser(description="Fine-tune DeepSeek Distill Qwen-2 on a dataset.")

    # Basic model/data params
    parser.add_argument(
        "--model_name",
        type=str,
        default="deepseek-ai/DeepSeek-R1-Distill-Qwen-1.5B",
        help="The model name or path on Hugging Face Hub."
    )
    parser.add_argument(
        "--dataset_name",
        type=str,
        default="deepmind/pg19", #wikitext
        help="Name of the dataset from the Hugging Face Hub (ignored if dataset_path is provided)."
    )
    parser.add_argument(
        "--dataset_path",
        type=str,
        default=None,
        help="Path to a local dataset. If provided, dataset_name will be ignored."
    )
    parser.add_argument(
        "--dataset_config",
        type=str,
        default=None, #wikitext-2-v1 
        help="Dataset configuration name (if needed)."
    )
    parser.add_argument(
        "--output_dir",
        type=str,
        default="./deepseek-qwen2-pg19-finetuned",
        help="Where to store the final model."
    )

    # Training hyperparameters
    parser.add_argument("--num_train_epochs", type=int, default=1, help="Number of training epochs.")
    parser.add_argument("--learning_rate", type=float, default=3e-5, help="Initial learning rate.")
    parser.add_argument("--per_device_train_batch_size", type=int, default=2, help="Train batch size.")
    parser.add_argument("--per_device_eval_batch_size", type=int, default=2, help="Eval batch size.")
    parser.add_argument("--gradient_accumulation_steps", type=int, default=4, help="Gradient accumulation steps.")
    parser.add_argument("--logging_steps", type=int, default=50, help="Log every X update steps.")
    parser.add_argument("--evaluation_strategy", type=str, default="steps", help="When to evaluate (epoch/steps).")
    parser.add_argument("--save_strategy", type=str, default="steps", help="When to save the model (epoch/steps).")
    parser.add_argument("--save_steps", type=int, default=500, help="Save checkpoint every X updates steps when using 'steps' strategy.")
    

    # Misc
    parser.add_argument("--block_size", type=int, default=1024, help="Block size for grouping text.")
    parser.add_argument("--max_train_samples", type=int, default=50000, help="For quick debugging, limit train samples.")
    parser.add_argument("--report_to", type=str, default="none", help="Reporting integration: 'none', 'wandb', etc.")
    parser.add_argument(
        "--cache_dir",
        type=str,
        default=None,
        help="Directory where to store the cached datasets. Defaults to ~/.cache/huggingface/datasets"
    )
    parser.add_argument(
        "--fp16",
        action="store_true",
        help="Use mixed-precision training if GPU is available."
    )

    return parser.parse_args()


def group_texts(examples, block_size):
    """
    Concatenate texts and split into blocks of size `block_size`.
    """
    concatenated = {k: sum(examples[k], []) for k in examples.keys()}
    total_length = len(concatenated["input_ids"])
    # drop the small remainder
    total_length = (total_length // block_size) * block_size
    return {
        k: [t[i : i + block_size] for i in range(0, total_length, block_size)]
        for k, t in concatenated.items()
    }


def main():
    start_time = time.time()
    args = parse_args()
    print("\n=== Starting Training Pipeline ===")    

    # 1. Load tokenizer and model
    print("\nStep 1: Loading tokenizer and model...")
    step_start = time.time()
    tokenizer = AutoTokenizer.from_pretrained("deepseek-ai/DeepSeek-R1-Distill-Qwen-1.5B", cache_dir=args.cache_dir)
    model = AutoModelForCausalLM.from_pretrained("deepseek-ai/DeepSeek-R1-Distill-Qwen-1.5B", cache_dir=args.cache_dir)
    print(f"Time taken: {time.time() - step_start:.2f} seconds")

    # 2. Load dataset
    print("\nStep 2: Loading dataset...")
    step_start = time.time()
    if args.dataset_path:
        print(f"Loading dataset from local path: {args.dataset_path}")
        data_files = {
            "train": os.path.join(args.dataset_path, "train", "train.json"),
            "validation": os.path.join(args.dataset_path, "validation", "valid.json"),
            "test": os.path.join(args.dataset_path, "test", "test.json")
        }
        raw_datasets = load_dataset(
            "json",
            data_files=data_files,
            cache_dir=args.cache_dir,
            trust_remote_code=True
        )
    else:
        print(f"Loading dataset from Hugging Face Hub: {args.dataset_name}")
        raw_datasets = load_dataset(
            args.dataset_name,
            args.dataset_config,
            cache_dir=args.cache_dir,
            trust_remote_code=True
        )
    print(f"Time taken: {time.time() - step_start:.2f} seconds")

    # 3. Tokenize
    print("\nStep 3: Tokenizing dataset...")
    step_start = time.time()
    def tokenize_function(examples):
        return tokenizer(examples["text"])

    tokenized_datasets = raw_datasets.map(
        tokenize_function,
        batched=True,
        num_proc=1,
        remove_columns=["text"],
    )

    # Optional: limit training set size for debugging
    if args.max_train_samples is not None:
        tokenized_datasets["train"] = tokenized_datasets["train"].select(range(args.max_train_samples))
    print(f"Time taken: {time.time() - step_start:.2f} seconds")

    # 4. Group text into blocks for causal LM
    print("\nStep 4: Grouping text into blocks...")
    step_start = time.time()
    lm_datasets = tokenized_datasets.map(
        lambda examples: group_texts(examples, args.block_size),
        batched=True,
    )
    print(f"Time taken: {time.time() - step_start:.2f} seconds")

    # 5. Data collator setup
    print("\nStep 5: Setting up data collator...")
    step_start = time.time()
    data_collator = DataCollatorForLanguageModeling(
        tokenizer=tokenizer, mlm=False
    )
    print(f"Time taken: {time.time() - step_start:.2f} seconds")

    # 6. Training setup
    print("\nStep 6: Setting up training arguments...")
    step_start = time.time()
    training_args = TrainingArguments(
        output_dir=args.output_dir,
        overwrite_output_dir=True,
        num_train_epochs=args.num_train_epochs,
        per_device_train_batch_size=args.per_device_train_batch_size,
        per_device_eval_batch_size=args.per_device_eval_batch_size,
        gradient_accumulation_steps=args.gradient_accumulation_steps,
        eval_steps=args.logging_steps,
        save_steps=args.save_steps,
        logging_steps=args.logging_steps,
        save_total_limit=3,
        fp16=args.fp16 and torch.cuda.is_available(),
        report_to=args.report_to,
        learning_rate=args.learning_rate,
        do_eval=True,
    )

    trainer = Trainer(
        model=model,
        args=training_args,
        train_dataset=lm_datasets["train"],
        eval_dataset=lm_datasets["validation"],
        tokenizer=tokenizer,
        data_collator=data_collator,
    )
    print(f"Time taken: {time.time() - step_start:.2f} seconds")

    # 7. Training
    print("\nStep 7: Starting training...")
    step_start = time.time()
    trainer.train()
    training_time = time.time() - step_start
    print(f"Training time: {training_time:.2f} seconds")

    # 8. Save final model
    print("\nStep 8: Saving final model...")
    step_start = time.time()
    trainer.save_model(args.output_dir)
    tokenizer.save_pretrained(args.output_dir)
    print(f"Time taken: {time.time() - step_start:.2f} seconds")

    total_time = time.time() - start_time
    print(f"\n=== Training Pipeline Completed ===")
    print(f"Total time taken: {total_time:.2f} seconds ({total_time/3600:.2f} hours)")
    print(f"Training time: {training_time:.2f} seconds ({training_time/3600:.2f} hours)")


if __name__ == "__main__":
    main()
