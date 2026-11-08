# Model Pricing Data

This directory contains a local copy of the mirrored model pricing data as a fallback mechanism.

## Source
The original file is maintained by the LiteLLM project and mirrored into the `price-mirror` branch of this repository via GitHub Actions:
- Mirror branch (configurable via `PRICE_MIRROR_REPO`): https://raw.githubusercontent.com/<your-repo>/price-mirror/model_prices_and_context_window.json
- Upstream source: https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json

## Purpose
This local copy serves as a fallback when the remote file cannot be downloaded due to:
- Network restrictions
- Firewall rules
- DNS resolution issues
- GitHub being blocked in certain regions
- Docker container network limitations

## Update Process
The pricingService will:
1. First attempt to download the latest version from GitHub
2. If download fails, use this local copy as fallback
3. Log a warning when using the fallback file

## Manual Update
To manually update this file with the latest pricing data (if automation is unavailable):
```bash
curl -s https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json -o model_prices_and_context_window.json
```

## File Format
The file contains JSON data with model pricing information including:
- Model names and identifiers
- Input/output token costs
- Context window sizes
- Model capabilities

Last updated: 2025-08-10

## Accounting Currency

Remote catalogs, this fallback file, `plaza_channel_catalog.json`, and
`pricing.override_file` use the original LiteLLM **USD** price format. Never
preconvert these files. The engine converts every absolute price to **CNY** once
when parsing it into memory, using `internal/pkg/currency.USDToCNY` (1 USD = 7 CNY).
Raw downloaded JSON, SHA-256 hashes and cache files remain unchanged in USD.
Reloads rebuild from those raw files, so they do not multiply an existing CNY
price again. Tier thresholds and all dimensionless multipliers are unchanged.
Administrator-entered channel/group prices are already CNY and are not converted
by the parser. Pricing catalog and model plaza official prices return CNY.
