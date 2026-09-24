// Package zalobot is an idiomatic Go SDK for the Zalo Bot Platform.
//
// It covers every method in the documented Bot API and offers two ways to
// receive updates:
//
//   - Long polling: construct a Bot and call [Bot.Start]. Updates are
//     delivered to handlers registered with [Bot.OnMessage], [Bot.OnText],
//     [Bot.OnCommand], and [Bot.OnEvent], and (best-effort) to the channel
//     returned by [Bot.Updates].
//   - Webhooks: mount [Bot.WebhookHandler] on an HTTP server. The handler
//     verifies the Zalo secret token in constant time and acknowledges
//     requests only after the update has been admitted for processing.
//
// A Bot is safe for concurrent use. Handlers for a single chat run in order;
// different chats are processed concurrently by a bounded worker pool.
//
// See the repository README and docs/ for guides, and the official API
// reference at https://docs.zaloplatforms.com/docs/BOT.
package zalobot
