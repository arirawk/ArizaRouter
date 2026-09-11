package registry

// iflowThinkingLevels lists the discrete reasoning levels accepted by iFlow
// models that expose a thinking toggle (mapped to on/off by the iflow applier).
var iflowThinkingLevels = []string{"none", "auto", "minimal", "low", "medium", "high", "xhigh"}

// GetIFlowModels returns the static iFlow (apis.iflow.cn) model definitions.
func GetIFlowModels() []*ModelInfo {
	model := func(id string, created int64, display, desc string, thinking bool) *ModelInfo {
		info := &ModelInfo{
			ID:          id,
			Object:      "model",
			Created:     created,
			OwnedBy:     "iflow",
			Type:        "iflow",
			DisplayName: display,
			Description: desc,
		}
		if thinking {
			info.Thinking = &ThinkingSupport{Levels: append([]string(nil), iflowThinkingLevels...)}
		}
		return info
	}
	return []*ModelInfo{
		model("qwen3-coder-plus", 1753228800, "Qwen3-Coder-Plus", "Qwen3 Coder Plus code generation", false),
		model("qwen3-max", 1758672000, "Qwen3-Max", "Qwen3 flagship model", false),
		model("qwen3-vl-plus", 1758672000, "Qwen3-VL-Plus", "Qwen3 multimodal vision-language", false),
		model("qwen3-max-preview", 1757030400, "Qwen3-Max-Preview", "Qwen3 Max preview build", true),
		model("glm-4.6", 1759190400, "GLM-4.6", "Zhipu GLM 4.6 general model", true),
		model("kimi-k2", 1752192000, "Kimi-K2", "Moonshot Kimi K2 general model", false),
		model("deepseek-v3.2", 1759104000, "DeepSeek-V3.2-Exp", "DeepSeek V3.2 experimental", true),
		model("deepseek-v3.1", 1756339200, "DeepSeek-V3.1-Terminus", "DeepSeek V3.1 Terminus", true),
		model("deepseek-r1", 1737331200, "DeepSeek-R1", "DeepSeek reasoning model R1", false),
		model("deepseek-v3", 1734307200, "DeepSeek-V3-671B", "DeepSeek V3 671B", false),
		model("qwen3-32b", 1747094400, "Qwen3-32B", "Qwen3 32B", false),
		model("qwen3-235b-a22b-thinking-2507", 1753401600, "Qwen3-235B-A22B-Thinking", "Qwen3 235B A22B Thinking (2507)", false),
		model("qwen3-235b-a22b-instruct", 1753401600, "Qwen3-235B-A22B-Instruct", "Qwen3 235B A22B Instruct", false),
		model("qwen3-235b", 1753401600, "Qwen3-235B-A22B", "Qwen3 235B A22B", false),
		model("iflow-rome-30ba3b", 1736899200, "iFlow-ROME", "iFlow Rome 30BA3B model", false),
	}
}
