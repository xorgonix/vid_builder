package animatic

import "vidbuild/internal/blocking"

// PlaybackDocument is the bounded, authorized animatic playback payload
// (spec §11.1, §14.5). It contains only the video's own structure: no
// credentials, filesystem paths or generation receipts.
type PlaybackDocument struct {
	VideoID          string         `json:"video_id"`
	Title            string         `json:"title"`
	TargetDurationMs int            `json:"target_duration_ms"`
	TotalDurationMs  int            `json:"total_duration_ms"`
	Status           string         `json:"status"`
	Revision         int            `json:"revision"`
	Settings         Settings       `json:"settings"`
	Shots            []PlaybackShot `json:"shots"`
}

// PlaybackShot is one shot in the playback document.
type PlaybackShot struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Purpose    string `json:"purpose"`
	SceneID    string `json:"scene_id"`
	SceneTitle string `json:"scene_title"`
	BeatID     string `json:"beat_id"`
	BeatTitle  string `json:"beat_title"`
	DurationMs int    `json:"duration_ms"`
	Revision   int    `json:"revision"`

	StartBlocking blocking.Spec `json:"start_blocking"`
	EndBlocking   blocking.Spec `json:"end_blocking"`
	TransitionOut Transition     `json:"transition_out"`
	Cues          []PlaybackCue  `json:"cues"`
	Metrics       map[string]int `json:"metrics,omitempty"`
}

// Transition is the outgoing transition shown when advancing past a shot.
type Transition struct {
	Type       string `json:"type"`
	DurationMs int    `json:"duration_ms"`
}

// PlaybackCue is one timed cue in the playback document.
type PlaybackCue struct {
	ID          string `json:"id"`
	CueType     string `json:"cue_type"`
	CharacterID string `json:"character_id,omitempty"`
	StartMs     int    `json:"start_ms"`
	DurationMs  int    `json:"duration_ms"`
	Content     string `json:"content"`
	Delivery    string `json:"delivery,omitempty"`
	Subtext     string `json:"subtext,omitempty"`
}
