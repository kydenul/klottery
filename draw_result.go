package lottery

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
