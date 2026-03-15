#!/usr/bin/env python3

import argparse
import os
import time
from dataclasses import dataclass
from datetime import timedelta

import torch
import torch.distributed as dist
import torch.nn as nn
import torch.nn.functional as F


@dataclass
class DistInfo:
    enabled: bool
    world_size: int
    rank: int
    local_rank: int


class TinyChatModel(nn.Module):
    def __init__(self, vocab_size: int, n_embd: int):
        super().__init__()
        self.tok_emb = nn.Embedding(vocab_size, n_embd)
        self.pos_emb = nn.Embedding(512, n_embd)
        self.ff = nn.Sequential(
            nn.Linear(n_embd, 4 * n_embd),
            nn.GELU(),
            nn.Linear(4 * n_embd, n_embd),
        )
        self.ln = nn.LayerNorm(n_embd)
        self.head = nn.Linear(n_embd, vocab_size)

    def forward(self, idx: torch.Tensor) -> torch.Tensor:
        positions = torch.arange(idx.size(1), device=idx.device).unsqueeze(0)
        x = self.tok_emb(idx) + self.pos_emb(positions)
        x = x + self.ff(x)
        x = self.ln(x)
        return self.head(x)


def maybe_init_dist(use_cuda: bool, timeout_s: int) -> DistInfo:
    world_size = int(os.environ.get("WORLD_SIZE", os.environ.get("SLURM_NTASKS", "1")))
    rank = int(os.environ.get("RANK", os.environ.get("SLURM_PROCID", "0")))
    local_rank = int(os.environ.get("LOCAL_RANK", os.environ.get("SLURM_LOCALID", "0")))
    if world_size <= 1:
        return DistInfo(False, 1, 0, 0)

    backend = "nccl" if use_cuda else "gloo"
    init_method = os.environ.get("FUSE_RDZV", "env://")
    if use_cuda:
        if local_rank >= torch.cuda.device_count():
            raise RuntimeError(
                f"LOCAL_RANK={local_rank} but only {torch.cuda.device_count()} CUDA devices are visible"
            )
        torch.cuda.set_device(local_rank)
    dist.init_process_group(
        backend=backend,
        init_method=init_method,
        world_size=world_size,
        rank=rank,
        timeout=timedelta(seconds=timeout_s),
    )
    return DistInfo(True, world_size, rank, local_rank)


def cleanup_dist(info: DistInfo):
    if info.enabled and dist.is_initialized():
        dist.destroy_process_group()


def main():
    parser = argparse.ArgumentParser(description="Tiny nanochat-style distributed smoke training.")
    parser.add_argument("--steps", type=int, default=40)
    parser.add_argument("--batch-size", type=int, default=8)
    parser.add_argument("--seq-len", type=int, default=128)
    parser.add_argument("--vocab-size", type=int, default=256)
    parser.add_argument("--n-embd", type=int, default=128)
    parser.add_argument("--lr", type=float, default=3e-4)
    parser.add_argument("--timeout-s", type=int, default=120)
    parser.add_argument("--seed", type=int, default=1337)
    args = parser.parse_args()

    torch.manual_seed(args.seed)
    use_cuda = torch.cuda.is_available()
    info = maybe_init_dist(use_cuda, args.timeout_s)
    device = torch.device(f"cuda:{info.local_rank}" if use_cuda else "cpu")

    try:
        model = TinyChatModel(args.vocab_size, args.n_embd).to(device)
        if info.enabled:
            model = nn.parallel.DistributedDataParallel(
                model,
                device_ids=[info.local_rank] if use_cuda else None,
            )
        optimizer = torch.optim.AdamW(model.parameters(), lr=args.lr)

        if info.rank == 0:
            print(
                f"device={device} distributed={info.enabled} world_size={info.world_size} rank={info.rank} local_rank={info.local_rank} steps={args.steps}",
                flush=True,
            )

        start = time.time()
        for step in range(1, args.steps + 1):
            x = torch.randint(args.vocab_size, (args.batch_size, args.seq_len), device=device)
            y = torch.randint(args.vocab_size, (args.batch_size, args.seq_len), device=device)
            logits = model(x)
            loss = F.cross_entropy(logits.view(-1, args.vocab_size), y.view(-1))
            optimizer.zero_grad(set_to_none=True)
            loss.backward()
            optimizer.step()

            if info.enabled:
                reduced = loss.detach().clone()
                dist.all_reduce(reduced, op=dist.ReduceOp.SUM)
                reduced /= info.world_size
            else:
                reduced = loss.detach()

            if info.rank == 0 and (step == 1 or step % 10 == 0 or step == args.steps):
                elapsed = max(time.time() - start, 1e-6)
                tokens = step * args.batch_size * args.seq_len * info.world_size
                print(
                    f"step={step:04d} loss={reduced.item():.4f} tok/s={int(tokens / elapsed)}",
                    flush=True,
                )

        if info.rank == 0:
            print("nanochat-smoke=ok", flush=True)
    finally:
        cleanup_dist(info)


if __name__ == "__main__":
    main()
