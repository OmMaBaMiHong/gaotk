package tokencenter

// ModelCapabilities preserves source declarations, with nil for unknown and
// pointers so an explicit false survives public and stored catalog round trips.
type ModelCapabilities struct {
	Reasoning        *bool `json:"reasoning,omitempty"`
	ToolCall         *bool `json:"toolCall,omitempty"`
	StructuredOutput *bool `json:"structuredOutput,omitempty"`
}
