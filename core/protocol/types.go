package protocol

// ObfsType тип медиа-обфускации RTP контейнера.
type ObfsType string

const (
	ObfsAudio ObfsType = "audio" // Opus, PayloadType 111, pad <= 24
	ObfsVideo ObfsType = "video" // VP8, PayloadType 96, pad <= 60
)
