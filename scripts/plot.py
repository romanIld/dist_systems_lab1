#!/usr/bin/env python3
"""Render the benchmark charts required by the report (section 5.2).

Input : results/summary.csv  (produced by cmd/benchmark)
Output: results/rtt_vs_messages.png
        results/throughput_vs_messages.png
        results/rtt_vs_concurrency.png
        results/throughput_vs_concurrency.png

Only matplotlib is required; CSV parsing uses the standard library.
"""

from __future__ import annotations

import argparse
import csv
import statistics
from collections import defaultdict
from pathlib import Path

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt

APPROACH_ORDER = ["threading", "async", "grpc"]
APPROACH_LABEL = {
    "threading": "1.1 blocking threads",
    "async": "1.2 async event loop",
    "grpc": "1.3 gRPC streaming",
}
MARKERS = {"threading": "o", "async": "s", "grpc": "^"}


def load(path: Path) -> list[dict]:
    with path.open(newline="", encoding="utf-8") as fh:
        return list(csv.DictReader(fh))


def aggregate(rows: list[dict], value_key: str, x_key: str, fixed_key: str, fixed_val: int):
    """Return {approach: [(x, mean_value), ...]} filtered to fixed_key == fixed_val,
    averaged over repeats and sorted by x."""
    bucket: dict[tuple[str, int], list[float]] = defaultdict(list)
    for r in rows:
        if int(r[fixed_key]) != fixed_val:
            continue
        key = (r["approach"], int(r[x_key]))
        bucket[key].append(float(r[value_key]))

    series: dict[str, list[tuple[int, float]]] = defaultdict(list)
    for (approach, x), values in bucket.items():
        series[approach].append((x, statistics.fmean(values)))
    for approach in series:
        series[approach].sort()
    return series


def pick_fixed(rows: list[dict], key: str, preferred: int) -> int:
    values = sorted({int(r[key]) for r in rows})
    return preferred if preferred in values else values[-1]


def line_plot(series, xlabel, ylabel, title, out_path: Path, logx: bool, logy: bool = False):
    fig, ax = plt.subplots(figsize=(7, 4.5))
    for approach in APPROACH_ORDER:
        pts = series.get(approach)
        if not pts:
            continue
        xs = [p[0] for p in pts]
        ys = [p[1] for p in pts]
        ax.plot(xs, ys, marker=MARKERS[approach], label=APPROACH_LABEL[approach])
    if logx:
        ax.set_xscale("log")
    if logy:
        ax.set_yscale("log")
    ax.set_xlabel(xlabel)
    ax.set_ylabel(ylabel)
    ax.set_title(title)
    ax.grid(True, which="both", linestyle=":", alpha=0.6)
    ax.legend()
    fig.tight_layout()
    fig.savefig(out_path, dpi=130)
    plt.close(fig)
    print(f"wrote {out_path}")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--csv", default="results/summary.csv", type=Path)
    parser.add_argument("--outdir", default="results", type=Path)
    parser.add_argument("--concurrency", type=int, default=100,
                        help="concurrency level for the *-vs-messages charts")
    parser.add_argument("--messages", type=int, default=1000,
                        help="message count for the *-vs-concurrency charts")
    args = parser.parse_args()

    rows = load(args.csv)
    if not rows:
        raise SystemExit(f"{args.csv} is empty")

    conc = pick_fixed(rows, "concurrency", args.concurrency)
    msgs = pick_fixed(rows, "messages", args.messages)
    args.outdir.mkdir(parents=True, exist_ok=True)

    line_plot(
        aggregate(rows, "mean_ms", "messages", "concurrency", conc),
        "messages per client", "mean RTT, ms (log scale)",
        f"RTT vs message count (concurrency = {conc})",
        args.outdir / "rtt_vs_messages.png", logx=True, logy=True,
    )
    line_plot(
        aggregate(rows, "throughput_msg_per_s", "messages", "concurrency", conc),
        "messages per client", "throughput, msg/s",
        f"Throughput vs message count (concurrency = {conc})",
        args.outdir / "throughput_vs_messages.png", logx=True,
    )
    line_plot(
        aggregate(rows, "mean_ms", "concurrency", "messages", msgs),
        "concurrent clients", "mean RTT, ms (log scale)",
        f"RTT vs concurrency (messages = {msgs})",
        args.outdir / "rtt_vs_concurrency.png", logx=False, logy=True,
    )
    line_plot(
        aggregate(rows, "throughput_msg_per_s", "concurrency", "messages", msgs),
        "concurrent clients", "throughput, msg/s",
        f"Throughput vs concurrency (messages = {msgs})",
        args.outdir / "throughput_vs_concurrency.png", logx=False,
    )


if __name__ == "__main__":
    main()
