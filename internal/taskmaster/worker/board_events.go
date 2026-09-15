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
}

// PublishBoardEvent marshals ev and publishes it on b. A nil broker is a
// no-op (tests that don't wire a board broker pass nil). Marshal errors are
// dropped defensively; BoardEvent always marshals cleanly.
func PublishBoardEvent(b *broker.Broker, ev BoardEvent) {
	if b == nil {
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
