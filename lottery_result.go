package lottery

import (
	"encoding/json"
)

// DrawError represents an error that occurred during a specific draw
type DrawError struct {
	DrawIndex int    `json:"draw_index"`    // Index of the draw that failed (1-based)
	Error     error  `json:"-"`             // The error that occurred (not serialized)
	ErrorMsg  string `json:"error_message"` // Error message for serialization
	Timestamp int64  `json:"timestamp"`     // Unix timestamp when the error occurred
}

// MarshalJSON implements custom JSON marshaling for DrawError
func (de DrawError) MarshalJSON() ([]byte, error) {
	// Create a temporary struct for marshaling
	temp := struct {
		DrawIndex int    `json:"draw_index"`
		ErrorMsg  string `json:"error_message"`
		Timestamp int64  `json:"timestamp"`
	}{
		DrawIndex: de.DrawIndex,
		ErrorMsg:  de.ErrorMsg,
		Timestamp: de.Timestamp,
	}

	// If ErrorMsg is empty but Error is not nil, use Error.Error()
	if temp.ErrorMsg == "" && de.Error != nil {
		temp.ErrorMsg = de.Error.Error()
	}

	return json.Marshal(temp)
}

// UnmarshalJSON implements custom JSON unmarshaling for DrawError
func (de *DrawError) UnmarshalJSON(data []byte) error {
	// Create a temporary struct for unmarshaling
	temp := struct {
		DrawIndex int    `json:"draw_index"`
		ErrorMsg  string `json:"error_message"`
		Timestamp int64  `json:"timestamp"`
	}{}

	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	de.DrawIndex = temp.DrawIndex
	de.ErrorMsg = temp.ErrorMsg
	de.Timestamp = temp.Timestamp
	// Note: Error field is not restored from JSON, only ErrorMsg is available

	return nil
}

// LotteryResult represents the result of a lottery draw
type LotteryResult struct {
	Success bool   `json:"success"`         // Whether the draw was successful
	Value   int    `json:"value,omitempty"` // Numeric result (for range lottery)
	Prize   *Prize `json:"prize,omitempty"` // Prize result (for prize pool lottery)
	Error   string `json:"error,omitempty"` // Error message if any
}

// MultiDrawResult represents the result of multiple lottery draws with error handling
type MultiDrawResult struct {
	Results        []int       `json:"results,omitempty"`       // Successful range draw results
	PrizeResults   []*Prize    `json:"prize_results,omitempty"` // Successful prize draw results
	TotalRequested int         `json:"total_requested"`         // Total number of draws requested
	Completed      int         `json:"completed"`               // Number of draws completed successfully
	Failed         int         `json:"failed"`                  // Number of draws that failed
	PartialSuccess bool        `json:"partial_success"`         // Whether this is a partial success
	LastError      error       `json:"last_error,omitempty"`    // Last error encountered
	ErrorDetails   []DrawError `json:"error_details,omitempty"` // Detailed error information for each failed draw
}

// DrawState represents the current state of a multi-draw operation
type DrawState struct {
	LockKey        string      `json:"lock_key"`                // The lock key being used
	TotalCount     int         `json:"total_count"`             // Total number of draws requested
	CompletedCount int         `json:"completed_count"`         // Number of draws completed
	Results        []int       `json:"results,omitempty"`       // Range draw results so far
	PrizeResults   []*Prize    `json:"prize_results,omitempty"` // Prize draw results so far
	Errors         []DrawError `json:"errors,omitempty"`        // Errors encountered so far
	StartTime      int64       `json:"start_time"`              // When the operation started
	LastUpdateTime int64       `json:"last_update_time"`        // When the state was last updated
}

// Validate validates the lottery result data
func (lr *LotteryResult) Validate() error {
	if !lr.Success && lr.Error == "" {
		return ErrInvalidParameters
	}
	if lr.Success && lr.Prize != nil {
		return lr.Prize.Validate()
	}
	return nil
}

// Validate validates the multi-draw result data
func (mdr *MultiDrawResult) Validate() error {
	if mdr.TotalRequested <= 0 {
		return ErrInvalidCount
	}
	if mdr.Completed < 0 || mdr.Failed < 0 {
		return ErrInvalidParameters
	}
	if mdr.Completed+mdr.Failed > mdr.TotalRequested {
		return ErrInvalidParameters
	}

	// Validate that we have the right number of results
	if len(mdr.Results) != mdr.Completed && len(mdr.PrizeResults) != mdr.Completed {
		return ErrInvalidParameters
	}

	return nil
}

// IsComplete returns true if all requested draws have been completed (successfully or with errors)
func (mdr *MultiDrawResult) IsComplete() bool {
	return mdr.Completed+mdr.Failed >= mdr.TotalRequested
}

// SuccessRate returns the success rate as a percentage
func (mdr *MultiDrawResult) SuccessRate() float64 {
	if mdr.TotalRequested == 0 {
		return 0.0
	}
	return float64(mdr.Completed) / float64(mdr.TotalRequested) * 100.0
}

// Validate validates the draw state data
func (ds *DrawState) Validate() error {
	if ds.LockKey == "" {
		return ErrInvalidParameters
	}
	if ds.TotalCount <= 0 {
		return ErrInvalidCount
	}
	if ds.CompletedCount < 0 || ds.CompletedCount > ds.TotalCount {
		return ErrInvalidParameters
	}
	if ds.StartTime <= 0 {
		return ErrInvalidParameters
	}
	return nil
}

// IsComplete returns true if all draws have been completed
func (ds *DrawState) IsComplete() bool {
	return ds.CompletedCount >= ds.TotalCount
}

// Progress returns the completion progress as a percentage
func (ds *DrawState) Progress() float64 {
	if ds.TotalCount == 0 {
		return 0.0
	}
	return float64(ds.CompletedCount) / float64(ds.TotalCount) * 100.0
}
