package tools

// bot_dispatch — unified message dispatch layer for chat bots.
//
// Implements per-user queuing, typing keep-alive, and thread-based reply
// strategy for long messages. Bridges platform-specific gateways (Discord,
// Slack) with the shared bot agent loop (bot_agent.go).
//
// Architecture:
//   discord_gateway.go + slack_gateway.go
//         ↓ (parseMessage → IncomingMessage)
//   dispatchMessage()  ← central entry point
//         ↓ (enqueue to per-user queue)
//   botQueues (per-user drains)
//         ↓ (keepTyping, RunBotAgent, sendReply)
//   bot_agent.go (shared agent loop)

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/caboose-mcp/server/config"
)

// botQueues manages per-user message queues to prevent concurrent execution
// for the same user. This ensures messages are processed serially, preserving
// conversation flow and avoiding race conditions in bot memory.
type botQueues struct {
	mu     sync.Mutex
	queues map[string]chan dispatchJob
}

// dispatchJob wraps a single message for execution by a queue drain goroutine.
type dispatchJob struct {
	ctx      context.Context
	cfg      *config.Config
	msg      IncomingMessage
	sender   PlatformSender
	provider ChatProvider
}

// newBotQueues creates an empty queue manager.
func newBotQueues() *botQueues {
	return &botQueues{queues: make(map[string]chan dispatchJob)}
}

// enqueue adds a job to the per-user queue. Returns false if the queue is
// at capacity (>= 3 pending messages), signaling the caller to inform the user.
// If the queue is new, starts a drain goroutine.
func (q *botQueues) enqueue(job dispatchJob) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Get or create the queue for this user
	ch, exists := q.queues[job.msg.UserKey]
	if !exists {
		ch = make(chan dispatchJob, 3) // buffer 3 messages
		q.queues[job.msg.UserKey] = ch
		// Start drain goroutine for this user
		go q.drain(job.msg.UserKey)
	}

	// Try to enqueue; return false if at capacity
	select {
	case ch <- job:
		return true
	default:
		return false
	}
}

// drain is a long-running goroutine that processes jobs for a single user,
// one at a time. Exits when the channel is closed (user becomes inactive).
func (q *botQueues) drain(userKey string) {
	ch := q.queues[userKey]
	for job := range ch {
		// Execute the job with the shared dispatch logic
		dispatchMessage(job.ctx, job.cfg, job.msg, job.sender, job.provider)
	}
}

// dispatchMessage is the central entry point for bot message handling.
// It coordinates typing indicators, agent execution, and reply delivery.
// Called by both discord_gateway and slack_gateway via botQueues.enqueue.
func dispatchMessage(ctx context.Context, cfg *config.Config, msg IncomingMessage, sender PlatformSender, provider ChatProvider) {
	// Start typing indicator in background
	stopTyping := keepTyping(ctx, sender, msg.ChannelID)
	defer stopTyping()

	// Run the shared bot agent loop
	reply, err := RunBotAgent(ctx, cfg, provider, msg.UserKey, msg.Content)
	stopTyping()

	// Handle errors from agent
	if err != nil {
		errorMsg := formatBotError(err)
		if _, sendErr := sender.SendText(msg.ChannelID, errorMsg); sendErr != nil {
			log.Printf("failed to send error message: %v", sendErr)
		}
		return
	}

	// Send the reply (may chunk or create thread for long replies)
	sendReply(ctx, sender, msg, reply)

	// Synthesize audio if the reply warrants it
	if ShouldSpeak(reply) {
		if audio, synthErr := Synthesize(ctx, cfg, reply); synthErr == nil && audio != nil {
			if audioErr := sender.SendAudio(msg.ChannelID, audio); audioErr != nil {
				log.Printf("failed to send audio: %v", audioErr)
			}
		} else if synthErr != nil {
			log.Printf("tts synthesize error: %v", synthErr)
		}
	}
}

// keepTyping starts a background ticker that sends typing indicators every
// 4 seconds until the returned CancelFunc is called. Returns a function that
// stops the ticker when called.
func keepTyping(ctx context.Context, sender PlatformSender, channelID string) context.CancelFunc {
	ctx2, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sender.SendTyping(channelID)
			case <-ctx2.Done():
				return
			}
		}
	}()
	return cancel
}

// sendReply handles the reply delivery strategy:
// - Short replies (< limit-200 chars): send inline
// - Long replies: try to create a thread with summary in main channel
// - Fallback if threading unsupported: chunk into multiple messages
func sendReply(ctx context.Context, sender PlatformSender, msg IncomingMessage, reply string) {
	limit := sender.MaxMessageLen()

	// Short reply: send inline in one message
	if len(reply) <= limit-200 {
		if _, err := sender.SendText(msg.ChannelID, reply); err != nil {
			log.Printf("failed to send reply: %v", err)
		}
		return
	}

	// Long reply: try to open a thread on the original message
	threadID, threadErr := sender.StartThread(msg.ChannelID, msg.OriginalMessageID, "⚔️ Response")

	if threadErr != nil || threadID == "" {
		// Threading unsupported or failed: fall back to chunking in main channel
		for _, chunk := range splitMessage(reply, limit) {
			if _, err := sender.SendText(msg.ChannelID, chunk); err != nil {
				log.Printf("failed to send reply chunk: %v", err)
			}
		}
		return
	}

	// Threading supported: send summary in main channel, full reply in thread
	summary := truncateText(reply, 200)
	summaryMsg := summary + " 📜 *Full response in thread ↓*"
	if _, err := sender.SendText(msg.ChannelID, summaryMsg); err != nil {
		log.Printf("failed to send summary: %v", err)
	}

	// Send full reply chunked in thread
	for _, chunk := range splitMessage(reply, limit) {
		if _, err := sender.SendText(threadID, chunk); err != nil {
			log.Printf("failed to send reply in thread: %v", err)
		}
	}

	// Send completion marker in thread
	if _, err := sender.SendText(threadID, "🗡️ *Done.*"); err != nil {
		log.Printf("failed to send done marker: %v", err)
	}
}

// formatBotError formats an error message for display to the user.
func formatBotError(err error) string {
	return "⚠️ *The ravens returned with troubling news:* `" + err.Error() + "`"
}

// truncateText returns at most the first n characters of s, appending "…" if truncated.
func truncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Global queue manager, initialized on first use
var globalBotQueues *botQueues
var queuesMu sync.Mutex

// getGlobalQueues returns the global botQueues instance, creating it if needed.
func getGlobalQueues() *botQueues {
	queuesMu.Lock()
	defer queuesMu.Unlock()
	if globalBotQueues == nil {
		globalBotQueues = newBotQueues()
	}
	return globalBotQueues
}

// EnqueueBotMessage adds a message to the bot dispatch queue.
// Called by discord_gateway and slack_gateway after parsing a message.
// Returns false if the user's queue is at capacity (should inform user to retry).
func EnqueueBotMessage(ctx context.Context, cfg *config.Config, msg IncomingMessage, sender PlatformSender, provider ChatProvider) bool {
	queues := getGlobalQueues()
	job := dispatchJob{
		ctx:      ctx,
		cfg:      cfg,
		msg:      msg,
		sender:   sender,
		provider: provider,
	}
	return queues.enqueue(job)
}
