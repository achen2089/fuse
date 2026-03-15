#!/usr/bin/env python3

import argparse
import random
import time

import torch
import torch.nn as nn
import torch.nn.functional as F


NAMES = [
    "emma", "olivia", "ava", "isabella", "sophia", "mia", "charlotte", "amelia",
    "harper", "evelyn", "abigail", "emily", "ella", "elizabeth", "camila", "luna",
    "sofia", "avery", "mila", "aria", "scarlett", "penelope", "layla", "chloe",
    "victoria", "madison", "eleanor", "grace", "nora", "riley", "zoey", "hannah",
    "hazel", "lily", "ellie", "violet", "nova", "aurora", "samantha", "maya",
    "leo", "jack", "luca", "theo", "asher", "james", "liam", "noah",
    "oliver", "elijah", "mateo", "lucas", "levi", "ezra", "logan", "mason",
    "sebastian", "alexander", "henry", "wyatt", "jackson", "hudson", "owen", "miles",
]


def build_dataset():
    chars = sorted({ch for name in NAMES for ch in name})
    stoi = {ch: idx + 1 for idx, ch in enumerate(chars)}
    stoi["."] = 0
    itos = {idx: ch for ch, idx in stoi.items()}
    xs, ys = [], []
    for name in NAMES:
        word = "." + name + "."
        for ch1, ch2 in zip(word, word[1:]):
            xs.append(stoi[ch1])
            ys.append(stoi[ch2])
    return torch.tensor(xs), torch.tensor(ys), stoi, itos


def sample_names(weight, itos, count, generator, device):
    samples = []
    for _ in range(count):
        idx = 0
        out = []
        while True:
            logits = weight[idx]
            probs = F.softmax(logits, dim=0)
            idx = torch.multinomial(probs, num_samples=1, generator=generator).item()
            if idx == 0:
                break
            out.append(itos[idx])
        samples.append("".join(out))
    return samples


def main():
    parser = argparse.ArgumentParser(description="Tiny makemore-style smoke training.")
    parser.add_argument("--steps", type=int, default=200)
    parser.add_argument("--batch-size", type=int, default=128)
    parser.add_argument("--lr", type=float, default=30.0)
    parser.add_argument("--seed", type=int, default=1337)
    parser.add_argument("--sample-count", type=int, default=8)
    args = parser.parse_args()

    random.seed(args.seed)
    torch.manual_seed(args.seed)
    device = torch.device("cuda" if torch.cuda.is_available() else "cpu")
    generator = torch.Generator(device="cpu").manual_seed(args.seed)

    xs, ys, _, itos = build_dataset()
    vocab_size = len(itos)
    model = nn.Embedding(vocab_size, vocab_size).to(device)
    optimizer = torch.optim.AdamW(model.parameters(), lr=args.lr)

    xs = xs.to(device)
    ys = ys.to(device)

    start = time.time()
    print(f"device={device} vocab={vocab_size} pairs={xs.numel()} steps={args.steps}", flush=True)

    for step in range(1, args.steps + 1):
        batch = torch.randint(0, xs.numel(), (args.batch_size,), device=device)
        logits = model(xs[batch])
        loss = F.cross_entropy(logits, ys[batch])
        optimizer.zero_grad(set_to_none=True)
        loss.backward()
        optimizer.step()

        if step == 1 or step % 25 == 0 or step == args.steps:
            elapsed = max(time.time() - start, 1e-6)
            tok_per_s = int((step * args.batch_size) / elapsed)
            print(f"step={step:04d} loss={loss.item():.4f} tok/s={tok_per_s}", flush=True)

    samples = sample_names(model.weight.detach().cpu(), itos, args.sample_count, generator, device)
    print("samples=" + ", ".join(samples), flush=True)


if __name__ == "__main__":
    main()
