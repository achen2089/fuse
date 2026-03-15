#!/usr/bin/env python3

import platform
import sys

import torch


def main():
    print(f"python={platform.python_version()}")
    print(f"torch={torch.__version__}")
    print(f"cuda_available={torch.cuda.is_available()}")
    if torch.cuda.is_available():
        print(f"gpu={torch.cuda.get_device_name(0)}")
    try:
        import axolotl  # type: ignore
    except Exception as exc:  # pragma: no cover - this is a runtime probe
        print(f"axolotl_import=failed error={exc}", file=sys.stderr)
        raise
    version = getattr(axolotl, "__version__", "unknown")
    print(f"axolotl={version}")


if __name__ == "__main__":
    main()
