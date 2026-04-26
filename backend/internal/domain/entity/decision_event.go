package entity

// RejectedSignalEvent fires when a SignalEvent is dropped before it can
// become an ApprovedSignalEvent. Stage tells the observer where in the
// pipeline the rejection happened (RejectedStageRisk / RejectedStageBookGate)
// and Reason carries the human-readable explanation copied from the
// rejecting handler. The struct exists only to give DecisionRecorder a way
// to observe rejections — the trading pipeline itself does not consume it.
type RejectedSignalEvent struct {
	Signal    Signal
	Stage     string
	Reason    string
	Price     float64
	Timestamp int64
}

func (e RejectedSignalEvent) EventType() string     { return EventTypeRejected }
func (e RejectedSignalEvent) EventTimestamp() int64 { return e.Timestamp }
