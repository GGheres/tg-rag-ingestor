package pricing

// Updated on 2026-03-12 using official pricing pages.
// Values below are stored as "money per 1M tokens" in fixed-point micros of the model currency.
// Example: $1.25 -> 1_250_000, $0.025 -> 25_000.
//
// Key rules reflected here:
//  1. Keep integer storage to avoid float rounding and preserve decimals.
//  2. cached input is stored separately where the provider publishes it.
//  3. reasoning/thinking price is 0 when the provider bills reasoning as output tokens.
//  4. Gemini "thinking tokens" are included in output pricing.
//  5. OpenAI reasoning tokens are billed as output tokens.
//  6. Moonshot prices are published in CNY, so currency is explicit.

const (
	PriceScale = uint64(1_000_000)

	CurrencyUSD = "USD"
	CurrencyCNY = "CNY"
)

func usd(v string) uint64 { return parseMoneyMicros(v) }
func cny(v string) uint64 { return parseMoneyMicros(v) }

func parseMoneyMicros(v string) uint64 {
	var whole uint64
	var frac uint64
	var fracDigits int
	seenDot := false

	for i := 0; i < len(v); i++ {
		ch := v[i]
		switch {
		case ch == '.':
			if seenDot {
				panic("invalid money value: " + v)
			}
			seenDot = true
		case ch >= '0' && ch <= '9':
			if !seenDot {
				whole = whole*10 + uint64(ch-'0')
			} else if fracDigits < 6 {
				frac = frac*10 + uint64(ch-'0')
				fracDigits++
			}
		default:
			panic("invalid money value: " + v)
		}
	}

	for fracDigits < 6 {
		frac *= 10
		fracDigits++
	}

	return whole*PriceScale + frac
}

const grokCurrentInfoPrompt = "Today is 2026-03-12. Use web search for current info."

type Pricing struct {
	Input       uint64 `json:"input"`
	CachedInput uint64 `json:"cached_input,omitempty"`
	Output      uint64 `json:"output"`

	// 0 means reasoning/thinking is billed using Output.
	Reasoning uint64 `json:"reasoning,omitempty"`

	// Optional flat or alternate output tiers for image/video models.
	WideOutput   uint64 `json:"wide_output,omitempty"`
	HDOutput     uint64 `json:"hd_output,omitempty"`
	HDWideOutput uint64 `json:"hd_wide_output,omitempty"`
	MediumOutput uint64 `json:"medium_output,omitempty"`
	SmallOutput  uint64 `json:"small_output,omitempty"`
}

type ModelInfo struct {
	ID                   string  `json:"id"`
	Name                 string  `json:"name"`
	Provider             string  `json:"provider"`
	Family               string  `json:"family"`
	Currency             string  `json:"currency,omitempty"`
	Pricing              Pricing `json:"pricing"`
	MaxOutput            int     `json:"max_output"`
	ContextWindow        int     `json:"context_window,omitempty"`
	RequiresResponsesAPI bool    `json:"requires_responses_api,omitempty"`
	BytesPerToken        float64 `json:"bytes_per_token,omitempty"`

	// Tiered pricing for long prompts.
	TierThreshold        int    `json:"tier_threshold,omitempty"`
	InputTierPrice       uint64 `json:"input_tier_price,omitempty"`
	CachedInputTierPrice uint64 `json:"cached_input_tier_price,omitempty"`
	OutputTierPrice      uint64 `json:"output_tier_price,omitempty"`

	Temperature        float32 `json:"temperature,omitempty"`
	ReasoningEffort    string  `json:"reasoning_effort,omitempty"`
	SystemPrompt       string  `json:"system_prompt,omitempty"`
	UseSearchGrounding bool    `json:"use_search_grounding,omitempty"`
}

// CostMicros returns cost in micro-units of model.Currency.
// inputTokens, cachedInputTokens, outputTokens, reasoningTokens are raw token counts.
func CostMicros(model ModelInfo, inputTokens, cachedInputTokens, outputTokens, reasoningTokens uint64) uint64 {
	p := model.Pricing

	inputRate := p.Input
	cachedRate := p.CachedInput
	outputRate := p.Output

	// Tier selection is based on prompt/input size.
	if model.TierThreshold > 0 && inputTokens > uint64(model.TierThreshold) {
		if model.InputTierPrice > 0 {
			inputRate = model.InputTierPrice
		}
		if model.CachedInputTierPrice > 0 {
			cachedRate = model.CachedInputTierPrice
		}
		if model.OutputTierPrice > 0 {
			outputRate = model.OutputTierPrice
		}
	}

	total := uint64(0)
	total += inputTokens * inputRate / 1_000_000
	total += cachedInputTokens * cachedRate / 1_000_000
	total += outputTokens * outputRate / 1_000_000

	if reasoningTokens > 0 {
		if p.Reasoning > 0 {
			total += reasoningTokens * p.Reasoning / 1_000_000
		} else {
			// OpenAI and Gemini bill reasoning/thinking as output.
			total += reasoningTokens * outputRate / 1_000_000
		}
	}

	return total
}

var LLM_MODELS = map[string]ModelInfo{
	// OpenAI
	"gpt-5.2": {
		ID: "gpt-5.2", Name: "GPT-5.2", Provider: "openai", Family: "gpt-5", Currency: CurrencyUSD,
		Pricing:   Pricing{Input: usd("1.75"), CachedInput: usd("0.175"), Output: usd("14.00")},
		MaxOutput: 128000, ContextWindow: 400000, BytesPerToken: 3.5,
		UseSearchGrounding: true,
	},
	"gpt-5.1": {
		ID: "gpt-5.1", Name: "GPT-5.1", Provider: "openai", Family: "gpt-5", Currency: CurrencyUSD,
		Pricing:   Pricing{Input: usd("1.25"), CachedInput: usd("0.125"), Output: usd("10.00")},
		MaxOutput: 128000, ContextWindow: 400000, BytesPerToken: 3.5,
		UseSearchGrounding: true,
	},
	"gpt-5-pro": {
		ID: "gpt-5-pro", Name: "GPT-5 Pro", Provider: "openai", Family: "gpt-5", Currency: CurrencyUSD,
		Pricing:   Pricing{Input: usd("15.00"), Output: usd("120.00")},
		MaxOutput: 272000, ContextWindow: 400000, RequiresResponsesAPI: true, BytesPerToken: 3.5,
		UseSearchGrounding: true,
	},
	"gpt-5-mini": {
		ID: "gpt-5-mini", Name: "GPT-5 Mini", Provider: "openai", Family: "gpt-5", Currency: CurrencyUSD,
		Pricing:   Pricing{Input: usd("0.25"), CachedInput: usd("0.025"), Output: usd("2.00")},
		MaxOutput: 128000, ContextWindow: 400000, BytesPerToken: 3.5,
		UseSearchGrounding: true,
	},
	"gpt-5-nano": {
		ID: "gpt-5-nano", Name: "GPT-5 Nano", Provider: "openai", Family: "gpt-5", Currency: CurrencyUSD,
		Pricing:   Pricing{Input: usd("0.05"), CachedInput: usd("0.005"), Output: usd("0.40")},
		MaxOutput: 128000, ContextWindow: 400000, BytesPerToken: 3.5,
		UseSearchGrounding: true,
	},
	"gpt-4o": {
		ID: "gpt-4o", Name: "GPT-4o", Provider: "openai", Family: "gpt-4", Currency: CurrencyUSD,
		Pricing:   Pricing{Input: usd("2.50"), CachedInput: usd("1.25"), Output: usd("10.00")},
		MaxOutput: 16384, ContextWindow: 128000, BytesPerToken: 3.5,
		UseSearchGrounding: true,
	},
	"gpt-4o-mini": {
		ID: "gpt-4o-mini", Name: "GPT-4o Mini", Provider: "openai", Family: "gpt-4", Currency: CurrencyUSD,
		Pricing:   Pricing{Input: usd("0.15"), CachedInput: usd("0.075"), Output: usd("0.60")},
		MaxOutput: 16384, ContextWindow: 128000, BytesPerToken: 3.5,
		UseSearchGrounding: true,
	},

	// Anthropic Claude
	"claude-opus-4-6": {
		ID: "claude-opus-4-6", Name: "Claude 4.6 Opus", Provider: "anthropic", Family: "claude", Currency: CurrencyUSD,
		Pricing:   Pricing{Input: usd("5.00"), CachedInput: usd("0.50"), Output: usd("25.00")},
		MaxOutput: 128000, ContextWindow: 200000, BytesPerToken: 3.5,
		UseSearchGrounding: true,
	},
	"claude-sonnet-4-6": {
		ID: "claude-sonnet-4-6", Name: "Claude 4.6 Sonnet", Provider: "anthropic", Family: "claude", Currency: CurrencyUSD,
		Pricing:   Pricing{Input: usd("3.00"), CachedInput: usd("0.30"), Output: usd("15.00")},
		MaxOutput: 64000, ContextWindow: 200000, BytesPerToken: 3.5,
		TierThreshold: 200000, InputTierPrice: usd("6.00"), OutputTierPrice: usd("22.50"),
		UseSearchGrounding: true,
	},
	"claude-sonnet-4-5": {
		ID: "claude-sonnet-4-5", Name: "Claude 4.5 Sonnet", Provider: "anthropic", Family: "claude", Currency: CurrencyUSD,
		Pricing:   Pricing{Input: usd("3.00"), CachedInput: usd("0.30"), Output: usd("15.00")},
		MaxOutput: 8192, ContextWindow: 400000, BytesPerToken: 3.5,
		TierThreshold: 200000, InputTierPrice: usd("6.00"), OutputTierPrice: usd("22.50"),
		UseSearchGrounding: true,
	},
	"claude-haiku-4-5": {
		ID: "claude-haiku-4-5", Name: "Claude 4.5 Haiku", Provider: "anthropic", Family: "claude", Currency: CurrencyUSD,
		Pricing:   Pricing{Input: usd("1.00"), CachedInput: usd("0.10"), Output: usd("5.00")},
		MaxOutput: 64000, ContextWindow: 200000, BytesPerToken: 3.5,
		UseSearchGrounding: true,
	},
	"claude-opus-4-5": {
		ID: "claude-opus-4-5", Name: "Claude 4.5 Opus", Provider: "anthropic", Family: "claude", Currency: CurrencyUSD,
		Pricing:   Pricing{Input: usd("5.00"), CachedInput: usd("0.50"), Output: usd("25.00")},
		MaxOutput: 4096, ContextWindow: 400000, BytesPerToken: 3.5,
		UseSearchGrounding: true,
	},

	// xAI Grok
	"grok-4-1-fast-reasoning": {
		ID: "grok-4-1-fast-reasoning", Name: "Grok 4.1 Fast Reasoning", Provider: "xai", Family: "grok", Currency: CurrencyUSD,
		Pricing:   Pricing{Input: usd("0.20"), CachedInput: usd("0.05"), Output: usd("0.50")},
		MaxOutput: 8192, ContextWindow: 2000000, BytesPerToken: 3.5,
		UseSearchGrounding: true,
		SystemPrompt:       grokCurrentInfoPrompt,
	},
	"grok-4-1-fast-non-reasoning": {
		ID: "grok-4-1-fast-non-reasoning", Name: "Grok 4.1 Fast Non-Reasoning", Provider: "xai", Family: "grok", Currency: CurrencyUSD,
		Pricing:   Pricing{Input: usd("0.20"), Output: usd("0.50")},
		MaxOutput: 8192, ContextWindow: 2000000, BytesPerToken: 3.5,
		UseSearchGrounding: true,
		SystemPrompt:       grokCurrentInfoPrompt,
	},
	"grok-4-fast-reasoning": {
		ID: "grok-4-fast-reasoning", Name: "Grok 4 Fast Reasoning", Provider: "xai", Family: "grok", Currency: CurrencyUSD,
		Pricing:   Pricing{Input: usd("0.20"), Output: usd("0.50")},
		MaxOutput: 8192, ContextWindow: 2000000, BytesPerToken: 3.5,
		UseSearchGrounding: true,
		SystemPrompt:       grokCurrentInfoPrompt,
	},
	"grok-4-fast-non-reasoning": {
		ID: "grok-4-fast-non-reasoning", Name: "Grok 4 Fast Non-Reasoning", Provider: "xai", Family: "grok", Currency: CurrencyUSD,
		Pricing:   Pricing{Input: usd("0.20"), Output: usd("0.50")},
		MaxOutput: 8192, ContextWindow: 2000000, BytesPerToken: 3.5,
		UseSearchGrounding: true,
		SystemPrompt:       grokCurrentInfoPrompt,
	},

	// DeepSeek
	"deepseek-chat": {
		ID: "deepseek-chat", Name: "DeepSeek Chat", Provider: "deepseek", Family: "deepseek", Currency: CurrencyUSD,
		Pricing:   Pricing{Input: usd("0.28"), CachedInput: usd("0.028"), Output: usd("0.42")},
		MaxOutput: 8000, ContextWindow: 128000, BytesPerToken: 3.0,
	},
	"deepseek-reasoner": {
		ID: "deepseek-reasoner", Name: "DeepSeek Reasoner", Provider: "deepseek", Family: "deepseek", Currency: CurrencyUSD,
		Pricing:   Pricing{Input: usd("0.55"), CachedInput: usd("0.14"), Output: usd("2.19")},
		MaxOutput: 64000, ContextWindow: 128000, BytesPerToken: 3.0,
	},

	// Gemini
	"gemini-3.1-pro-preview": {
		ID: "gemini-3.1-pro-preview", Name: "Gemini 3.1 Pro Preview", Provider: "google", Family: "gemini", Currency: CurrencyUSD,
		Pricing:   Pricing{Input: usd("2.00"), CachedInput: usd("0.20"), Output: usd("12.00")},
		MaxOutput: 65000, ContextWindow: 1000000, BytesPerToken: 4.0,
		TierThreshold: 200000, InputTierPrice: usd("4.00"), CachedInputTierPrice: usd("0.40"), OutputTierPrice: usd("18.00"),
		UseSearchGrounding: true,
	},
	"gemini-3.1-flash-lite-preview": {
		ID: "gemini-3.1-flash-lite-preview", Name: "Gemini 3.1 Flash Lite Preview", Provider: "google", Family: "gemini", Currency: CurrencyUSD,
		Pricing:   Pricing{Input: usd("0.25"), CachedInput: usd("0.025"), Output: usd("1.50")},
		MaxOutput: 65000, ContextWindow: 1000000, BytesPerToken: 4.0,
		UseSearchGrounding: true,
	},
	"gemini-3-flash-preview": {
		ID: "gemini-3-flash-preview", Name: "Gemini 3 Flash Preview", Provider: "google", Family: "gemini", Currency: CurrencyUSD,
		Pricing:   Pricing{Input: usd("0.50"), CachedInput: usd("0.05"), Output: usd("3.00")},
		MaxOutput: 65000, ContextWindow: 1000000, BytesPerToken: 4.0,
		UseSearchGrounding: true,
	},
	"gemini-2.5-pro": {
		ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", Provider: "google", Family: "gemini", Currency: CurrencyUSD,
		Pricing:   Pricing{Input: usd("1.25"), CachedInput: usd("0.125"), Output: usd("10.00")},
		MaxOutput: 65000, ContextWindow: 1000000, BytesPerToken: 4.0,
		TierThreshold: 200000, InputTierPrice: usd("2.50"), CachedInputTierPrice: usd("0.25"), OutputTierPrice: usd("15.00"),
		UseSearchGrounding: true,
	},
	"gemini-2.5-flash": {
		ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash", Provider: "google", Family: "gemini", Currency: CurrencyUSD,
		Pricing:   Pricing{Input: usd("0.30"), CachedInput: usd("0.03"), Output: usd("2.50")},
		MaxOutput: 65000, ContextWindow: 1000000, BytesPerToken: 4.0,
		UseSearchGrounding: true,
	},
	"gemini-2.5-flash-lite": {
		ID: "gemini-2.5-flash-lite", Name: "Gemini 2.5 Flash Lite", Provider: "google", Family: "gemini", Currency: CurrencyUSD,
		Pricing:   Pricing{Input: usd("0.10"), CachedInput: usd("0.01"), Output: usd("0.40")},
		MaxOutput: 65000, ContextWindow: 1000000, BytesPerToken: 4.0,
		UseSearchGrounding: true,
	},

	// RAG service models
	"text-embedding-004": {
		ID: "text-embedding-004", Name: "Gemini Embedding v4", Provider: "google", Family: "embedding", Currency: CurrencyUSD,
		Pricing: Pricing{Input: usd("0.15"), Output: 0}, ContextWindow: 2048,
	},
	"gemini-embedding-001": {
		ID: "gemini-embedding-001", Name: "Gemini Embedding v1", Provider: "google", Family: "embedding", Currency: CurrencyUSD,
		Pricing: Pricing{Input: usd("0.15"), Output: 0}, ContextWindow: 2048,
	},
	"rerank-multilingual-v3.0": {
		ID: "rerank-multilingual-v3.0", Name: "Cohere Rerank v3", Provider: "cohere", Family: "reranker", Currency: CurrencyUSD,
		Pricing: Pricing{Input: 0, Output: usd("2.00")},
	},

	// KIMI / Moonshot
	"kimi-k2-thinking": {
		ID: "kimi-k2-thinking", Name: "Kimi K2 Thinking", Provider: "moonshot", Family: "kimi", Currency: CurrencyCNY,
		Pricing:   Pricing{CachedInput: cny("1.00"), Input: cny("4.00"), Output: cny("16.00")},
		MaxOutput: 8192, ContextWindow: 262000, BytesPerToken: 3.0,
		UseSearchGrounding: true,
	},
	"kimi-k2-thinking-turbo": {
		ID: "kimi-k2-thinking-turbo", Name: "Kimi K2 Thinking Turbo", Provider: "moonshot", Family: "kimi", Currency: CurrencyCNY,
		Pricing:   Pricing{CachedInput: cny("1.00"), Input: cny("8.00"), Output: cny("58.00")},
		MaxOutput: 8192, ContextWindow: 262000, BytesPerToken: 3.0,
		UseSearchGrounding: true,
	},
	"kimi-k2.5": {
		ID: "kimi-k2.5", Name: "Kimi K2.5", Provider: "moonshot", Family: "kimi", Currency: CurrencyCNY,
		Pricing:   Pricing{CachedInput: cny("0.70"), Input: cny("4.00"), Output: cny("21.00")},
		MaxOutput: 8192, ContextWindow: 262000, BytesPerToken: 3.0,
		UseSearchGrounding: true,
	},

	// OpenAI image generation
	"dall-e-3": {
		ID:            "dall-e-3",
		Name:          "DALL-E 3",
		Provider:      "openai-image",
		Family:        "image-generation",
		Currency:      CurrencyUSD,
		Pricing:       Pricing{Output: usd("0.040"), WideOutput: usd("0.080"), HDOutput: usd("0.080"), HDWideOutput: usd("0.120")},
		MaxOutput:     1,
		ContextWindow: 4000,
	},
	"dall-e-2": {
		ID:            "dall-e-2",
		Name:          "DALL-E 2",
		Provider:      "openai-image",
		Family:        "image-generation",
		Currency:      CurrencyUSD,
		Pricing:       Pricing{Output: usd("0.020"), MediumOutput: usd("0.018"), SmallOutput: usd("0.016")},
		MaxOutput:     1,
		ContextWindow: 1000,
	},

	// Google native image generation
	"gemini-3.1-flash-image-preview": {
		ID:            "gemini-3.1-flash-image-preview",
		Name:          "Nano Banana 2",
		Provider:      "google-image-native",
		Family:        "image-generation",
		Currency:      CurrencyUSD,
		Pricing:       Pricing{Input: usd("0.50"), Output: usd("3.90")},
		MaxOutput:     1,
		ContextWindow: 32768,
	},
	"gemini-2.5-flash-image": {
		ID:            "gemini-2.5-flash-image",
		Name:          "Nano Banana",
		Provider:      "google-image-native",
		Family:        "image-generation",
		Currency:      CurrencyUSD,
		Pricing:       Pricing{Input: usd("0.30"), Output: usd("3.90")},
		MaxOutput:     1,
		ContextWindow: 32768,
	},
	"imagen-4.0-ultra-generate-001": {
		ID:            "imagen-4.0-ultra-generate-001",
		Name:          "Imagen 4 Ultra",
		Provider:      "google-image",
		Family:        "image-generation",
		Currency:      CurrencyUSD,
		Pricing:       Pricing{Input: 0, Output: usd("6.00")},
		MaxOutput:     1,
		ContextWindow: 2048,
	},

	// Video generation
	"sora-2": {
		ID:            "sora-2",
		Name:          "Sora 2",
		Provider:      "openai-video",
		Family:        "video-generation",
		Currency:      CurrencyUSD,
		Pricing:       Pricing{Input: 0, Output: usd("0.10")}, // per second
		MaxOutput:     1,
		ContextWindow: 2048,
		SystemPrompt:  "allow_reference_image,personGeneration=allow_all",
	},
	"sora-2-pro": {
		ID:            "sora-2-pro",
		Name:          "Sora 2 Pro",
		Provider:      "openai-video",
		Family:        "video-generation",
		Currency:      CurrencyUSD,
		Pricing:       Pricing{Input: 0, Output: usd("0.30")}, // base per second
		MaxOutput:     1,
		ContextWindow: 2048,
		SystemPrompt:  "allow_reference_image,personGeneration=allow_all",
	},
	"veo-3.1-generate-preview": {
		ID:            "veo-3.1-generate-preview",
		Name:          "Veo 3.1",
		Provider:      "google-video",
		Family:        "video-generation",
		Currency:      CurrencyUSD,
		Pricing:       Pricing{Input: 0, Output: usd("0.50")}, // per second equivalent used by current app logic
		MaxOutput:     1,
		ContextWindow: 1024,
		SystemPrompt:  "allow_reference_image,personGeneration=allow_all",
	},
	"veo-3.1-fast-generate-preview": {
		ID:            "veo-3.1-fast-generate-preview",
		Name:          "Veo 3.1 Fast",
		Provider:      "google-video",
		Family:        "video-generation",
		Currency:      CurrencyUSD,
		Pricing:       Pricing{Input: 0, Output: usd("0.30")}, // per second equivalent used by current app logic
		MaxOutput:     1,
		ContextWindow: 1024,
		SystemPrompt:  "allow_reference_image,personGeneration=allow_all",
	},
}
