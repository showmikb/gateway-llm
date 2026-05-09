package types

type SpeechRequest struct {
	Model          string  `json:"model"`
	Input          string  `json:"input"`
	Voice          string  `json:"voice"`
	ResponseFormat string  `json:"response_format,omitempty"`
	Speed          float64 `json:"speed,omitempty"`
}

type TranscriptionResponse struct {
	Text     string               `json:"text"`
	Language string               `json:"language,omitempty"`
	Duration float64              `json:"duration,omitempty"`
	Segments []TranscriptionSegment `json:"segments,omitempty"`
}

type TranscriptionSegment struct {
	ID    int     `json:"id"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}
