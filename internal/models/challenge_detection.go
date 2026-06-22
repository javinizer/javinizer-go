package models

import "github.com/javinizer/javinizer-go/internal/challengedetect"

// IsCloudflareChallengePage detects Cloudflare anti-bot/interstitial challenge pages.
//
// Deprecated: Use challengedetect.IsCloudflareChallengePage directly.
// This alias is kept for backward compatibility with existing callers.
var IsCloudflareChallengePage = challengedetect.IsCloudflareChallengePage
