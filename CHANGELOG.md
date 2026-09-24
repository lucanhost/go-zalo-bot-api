# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-09-24

### Added

- Initial release of the `zalobot` Go SDK for the Zalo Bot Platform.
- Full coverage of the documented Bot API: `getMe`, `getUpdates`,
  `setWebhook`, `deleteWebhook`, `getWebhookInfo`, `testWebhook`,
  `sendMessage`, `sendPhoto`, `sendSticker`, `sendVoice`, and
  `sendChatAction`.
- Typed update/message models with tolerant decoding and compatibility-field
  normalization.
- Rich-text helpers (`parse_mode` and `text_styles`) with UTF-16-correct
  `StyleRange` / `StyleRangeN`.
- Long-polling client with 408-as-idle, fatal-401, jittered backoff, and a
  tight-loop guard.
- Webhook `http.Handler` with constant-time secret verification and
  acknowledge-after-admission semantics.
- Per-chat dispatcher with a bounded worker pool, FIFO scheduling, and a
  bounded quantum so no chat monopolizes a worker.
- Explicit lifecycle: `Start`, `Shutdown`, `Stop`, `Done`, and `Err`.
- Typed error taxonomy (`APIError`, `TransportError`, `DecodeError`,
  `ValidationError`) with status predicates and token redaction.
- Hermetic race/goleak test suite plus an opt-in live integration suite.

[Unreleased]: https://github.com/lucanhost/go-zalo-bot-api/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/lucanhost/go-zalo-bot-api/releases/tag/v0.1.0
