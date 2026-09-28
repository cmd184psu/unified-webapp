package worker

import (
	"encoding/json"

	"cmd184psu/unified-webapp/internal/platform/broker"
)

// BoardEvent is the compact payload published on the shared board-events
// broker (GET /api/board/events) whenever something changes that affects
// the lane board (FRD §8a / plan D7). Only the fields relevant to Type are
// set; the rest are omitted from the JSON so payloads stay small. Defined
// once here so both the worker and the coordinator publish the same shape.
type BoardEvent struct {
	Type        string `json:"type"`
	Lane        string `json:"lane,omitempty"`
	Task        string `json:"task,omitempty"`
	ExecutionID int64  `json:"execution_id,omitempty"`
	Status      string `json:"status,omitempty"`
	Engaged     *bool  `json:"engaged,omitempty"`
	// ProgressPct/ProgressLabel carry a "task-progress" event's payload
	// (func-task progress, plan §4.9); unset for every other event type.
	ProgressPct   *int   `json:"progress_pct,omitempty"`
	ProgressLabel string `json:"progress_label,omitempty"`
}

// PublishBoardEvent marshals ev and publishes it on b, dropping it if ev.Lane
// names a currently hidden lane (N4) — a hidden lane's tasks/executions are
// invisible to the board, so its events must be too. A nil broker is a
// no-op (tests that don't wire a board broker pass nil); a nil hidden set
// hides nothing, same as HiddenLanes.Hidden's nil receiver. Marshal errors
// are dropped defensively; BoardEvent always marshals cleanly.
func PublishBoardEvent(b *broker.Broker, h *HiddenLanes, ev BoardEvent) {
	if b == nil {
		return
	}
	if ev.Lane != "" && h.Hidden(ev.Lane) {
		return
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	b.Publish(string(data))
}

// EngagedEvent builds a "brake" BoardEvent with the engaged field set.
func EngagedEvent(engaged bool) BoardEvent {
	return BoardEvent{Type: "brake", Engaged: &engaged}
}
